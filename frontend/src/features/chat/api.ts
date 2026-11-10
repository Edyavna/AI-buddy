import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import { conversationSchema, messageSchema } from './schemas';

export async function getData<T>(path: string, schema: z.ZodType<T>, signal?: AbortSignal): Promise<T> {
  const result = await apiRequest(path, schema, signal ? { signal } : {});
  if (result.data === undefined) throw new ClientError('protocol', 'Ответ сервера пуст');
  return result.data;
}
export const listConversations = (signal?: AbortSignal) => getData('/conversations', z.array(conversationSchema), signal);
export const getConversation = (id: string, signal?: AbortSignal) => getData(`/conversations/${encodeURIComponent(id)}`, conversationSchema, signal);
export const listMessages = (id: string, signal?: AbortSignal) => getData(`/conversations/${encodeURIComponent(id)}/messages`, z.array(messageSchema), signal);
export async function createConversation(id: string, stepId: string | null, signal: AbortSignal) {
  const result = await apiRequest('/conversations', conversationSchema, {
    method: 'POST', signal, body: { client_conversation_id: id, ...(stepId && { step_id: stepId }) },
  });
  if (!result.data) throw new ClientError('protocol', 'Диалог не создан');
  return result.data;
}
