import type { QueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import type { MentorQuestion, MentorQuestionLookup } from './schemas';

export const mentorQuestionKeys = {
  all: ['mentor-questions'] as const,
  byMessage: (messageId: string) => queryKeys.mentorQuestion(messageId),
  mutation: (messageId: string) => ['mentor-questions', 'create', messageId] as const,
};

export function acceptMentorQuestion(
  queryClient: QueryClient,
  messageId: string,
  incoming: MentorQuestion | null,
): MentorQuestionLookup {
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
