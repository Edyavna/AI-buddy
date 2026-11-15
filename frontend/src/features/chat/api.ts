import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import { conversationSchema, messageSchema } from './schemas';

export async function getData<T>(path: string, schema: z.ZodType<T>, signal?: AbortSignal, search?: Record<string, string>): Promise<T> {
  const result = await apiRequest(path, schema, { ...(signal && { signal }), ...(search && { search }) });
  if (result.data === undefined) throw new ClientError('protocol', 'Ответ сервера пуст');
  return result.data;
}
export const listConversations = (signal?: AbortSignal, archived = false) => getData('/conversations', z.array(conversationSchema), signal, archived ? { archived: 'true' } : undefined);
export const setConversationArchived = (id: string, archived: boolean, signal: AbortSignal) =>
  apiRequest(`/conversations/${encodeURIComponent(id)}`, conversationSchema, { method: 'PATCH', body: { archived }, signal });
export const getConversation = (id: string, signal?: AbortSignal) => getData(`/conversations/${encodeURIComponent(id)}`, conversationSchema, signal);
export const listMessages = (id: string, signal?: AbortSignal) => getData(`/conversations/${encodeURIComponent(id)}/messages`, z.array(messageSchema), signal);
export async function createConversation(id: string, stepId: string | null, signal: AbortSignal, markdown = false) {
  const result = await apiRequest('/conversations', conversationSchema, {
    method: 'POST', signal, body: { client_conversation_id: id, ...(stepId && { step_id: stepId }), ...(markdown && { markdown }) },
  });
  if (!result.data) throw new ClientError('protocol', 'Диалог не создан');
  return result.data;
}
