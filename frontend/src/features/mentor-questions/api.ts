import { apiRequest, ClientError } from '@/shared/api';
import { mentorQuestionLookupSchema, mentorQuestionSchema } from './schemas';
import type { MentorQuestion, MentorQuestionLookup } from './schemas';

function questionPath(messageId: string): string {
  return `/messages/${encodeURIComponent(messageId)}/knowledge-gap`;
}

export const mentorQuestionsApi = {
  async get(messageId: string, signal: AbortSignal): Promise<MentorQuestionLookup> {
    const { data } = await apiRequest(questionPath(messageId), mentorQuestionLookupSchema, { signal });
    if (!data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
    return data;
  },

  async create(messageId: string, signal: AbortSignal): Promise<MentorQuestion> {
    const { data } = await apiRequest(questionPath(messageId), mentorQuestionSchema, {
      method: 'POST',
      signal,
    });
    if (!data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
    return data;
  },
};
