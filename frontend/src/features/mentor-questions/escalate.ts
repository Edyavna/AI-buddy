import type { QueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { mentorQuestionsApi } from './api';
import { acceptMentorQuestion } from './cache';
import type { MentorQuestion } from './schemas';

const inflight = new Map<string, Promise<MentorQuestion>>();
const controllers = new Map<string, AbortController>();

export function cancelMentorQuestionRequests(): void {
  controllers.forEach(controller => controller.abort());
}

export function escalateMentorQuestionShared(
  queryClient: QueryClient,
  messageId: string,
): Promise<MentorQuestion> {
  const pending = inflight.get(messageId);
  if (pending) {
    return pending.then(question => {
      acceptMentorQuestion(queryClient, messageId, question);
      return question;
    });
  }

  const controller = new AbortController();
  const task = (async (): Promise<MentorQuestion> => {
    let question: MentorQuestion;
    try {
      question = await mentorQuestionsApi.create(messageId, controller.signal);
    } catch (error) {
      if (!(error instanceof ClientError) || error.kind !== 'network') throw error;

      const restored = await mentorQuestionsApi.get(messageId, controller.signal);
      if (!restored.question) throw error;
      question = restored.question;
    }

    acceptMentorQuestion(queryClient, messageId, question);
    return question;
  })().finally(() => {
    if (inflight.get(messageId) === task) {
      inflight.delete(messageId);
      controllers.delete(messageId);
    }
  });

  inflight.set(messageId, task);
  controllers.set(messageId, controller);
  return task;
}
