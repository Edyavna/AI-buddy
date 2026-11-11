import type { QueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { sessionSignal } from '@/shared/api/session-signal';
import { mentorQuestionsApi } from './api';
import { acceptMentorQuestion, isAccessError, mentorQuestionKeys, revokeMentorQuestion } from './cache';
import type { MentorQuestion } from './schemas';

const inflight = new WeakMap<AbortSignal, Map<string, Promise<MentorQuestion>>>();

export function escalateMentorQuestionShared(
  queryClient: QueryClient,
  messageId: string,
): Promise<MentorQuestion> {
  const session = sessionSignal(queryClient);
  let operations = inflight.get(session);
  if (!operations) {
    operations = new Map();
    inflight.set(session, operations);
  }
  const pending = operations.get(messageId);
  if (pending) return pending;

  const key = mentorQuestionKeys.byMessage(messageId);
  const task = (async () => {
    let question: MentorQuestion;
    const timeout = AbortSignal.timeout(10_000);
    try {
      question = await mentorQuestionsApi.create(messageId, AbortSignal.any([session, timeout]));
    } catch (error) {
      session.throwIfAborted();
      const uncertain = timeout.aborted || error instanceof ClientError &&
        (error.kind === 'network' || error.kind === 'protocol' || (error.status ?? 0) >= 500);
      if (!uncertain) throw error;

      // Reconcile with a GET started after the uncertain POST.
      await queryClient.cancelQueries({ queryKey: key, exact: true });
      session.throwIfAborted();
      const restored = await mentorQuestionsApi.get(messageId, AbortSignal.any([session, AbortSignal.timeout(10_000)]));
      session.throwIfAborted();
      acceptMentorQuestion(queryClient, messageId, restored.question);
      if (!restored.question) {
        if (timeout.aborted) {
          throw new Error('Не удалось подтвердить передачу. Повторите попытку', { cause: error });
        }
        throw error;
      }
      question = restored.question;
    }

    session.throwIfAborted();
    acceptMentorQuestion(queryClient, messageId, question);
    return question;
  })().catch(async error => {
    if (!session.aborted && isAccessError(error)) {
      await queryClient.cancelQueries({ queryKey: key, exact: true }, { revert: false });
      if (!session.aborted) revokeMentorQuestion(queryClient, messageId);
    }
    throw error;
  }).finally(() => operations.delete(messageId));

  operations.set(messageId, task);
  return task;
}
