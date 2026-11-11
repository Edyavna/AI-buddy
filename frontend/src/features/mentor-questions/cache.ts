import type { QueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import type { MentorQuestion, MentorQuestionLookup } from './schemas';

export type MentorQuestionCache = MentorQuestionLookup & { accessDenied?: boolean };

export function isAccessError(error: unknown): boolean {
  return error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);
}

export function revokeMentorQuestion(queryClient: QueryClient, messageId: string): void {
  queryClient.setQueryData<MentorQuestionCache>(mentorQuestionKeys.byMessage(messageId), {
    question: null,
    accessDenied: true,
  });
}

export const mentorQuestionKeys = {
  all: ['mentor-questions'] as const,
  byMessage: (messageId: string) => queryKeys.mentorQuestion(messageId),
  mutation: (messageId: string) => ['mentor-questions', 'create', messageId] as const,
};

export function acceptMentorQuestion(
  queryClient: QueryClient,
  messageId: string,
  incoming: MentorQuestion | null,
): MentorQuestionCache {
  if (incoming && incoming.message_id !== messageId) {
    throw new ClientError('protocol', 'Сервер вернул вопрос для другого сообщения');
  }

  const key = mentorQuestionKeys.byMessage(messageId);
  const cached = queryClient.getQueryData<MentorQuestionLookup>(key);
  if (cached?.question && (!incoming || incoming.revision < cached.question.revision)) {
    return cached;
  }

  const next = { question: incoming };
  queryClient.setQueryData(key, next);
  return next;
}
