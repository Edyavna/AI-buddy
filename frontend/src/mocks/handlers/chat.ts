import { http, HttpResponse } from 'msw';
import { z } from 'zod';
import { createScenario } from '../fixtures/scenario';
import type { Conversation } from '../../shared/api';
import { messageSchema } from '../../features/chat/schemas';

const createConversationSchema = z.object({
  client_conversation_id: z.uuid(), step_id: z.string().nullable().optional(),
}).strict();

// State is scoped to the handler factory/test. No random transitions or timers.
export function createChatHandlers() {
  const fixture = createScenario();
  const conversations: Conversation[] = [fixture.conversation];
  const histories = new Map<string, ReturnType<typeof messageSchema.parse>[]>([
    [fixture.conversation.id, fixture.messages.map(message => messageSchema.parse(message))],
  ]);
  return [
    http.get('*/api/v1/conversations', () => HttpResponse.json(conversations)),
    http.post('*/api/v1/conversations', async ({ request }) => {
      const parsed = createConversationSchema.safeParse(await request.json());
      if (!parsed.success) return HttpResponse.json({ error: {
        code: 'VALIDATION_ERROR', message: 'Некорректный запрос', trace_id: 'trace_mock',
      } }, { status: 422 });
      const input = parsed.data;
      const existing = conversations.find(c => c.client_conversation_id === input.client_conversation_id);
      if (existing) {
        if (existing.step_id !== (input.step_id ?? null)) return HttpResponse.json({ error: {
          code: 'IDEMPOTENCY_CONFLICT', message: 'Параметры изменились', trace_id: 'trace_mock',
        } }, { status: 409 });
        return HttpResponse.json(existing, { status: 200 });
      }
      const conversation = {
        ...fixture.conversation, id: `conversation_${input.client_conversation_id}`,
        client_conversation_id: input.client_conversation_id, step_id: input.step_id ?? null,
      };
      conversations.push(conversation);
      return HttpResponse.json(conversation, { status: 201 });
    }),
    http.get('*/api/v1/conversations/:id', ({ params }) => {
      const found = conversations.find(c => c.id === params.id);
      return found ? HttpResponse.json(found) : notFound();
    }),
    http.get('*/api/v1/conversations/:id/messages', ({ params }) => {
      if (!conversations.some(c => c.id === params.id)) return notFound();
      return HttpResponse.json(histories.get(String(params.id)) ?? []);
    }),
    http.post('*/api/v1/conversations/:id/messages', async ({ params, request }) => {
      const id = String(params.id);
      if (!conversations.some(c => c.id === id)) return notFound();
      const parsed = z.object({ client_message_id: z.uuid(), text: z.string().min(1).max(8000).regex(/\S/) }).strict().safeParse(await request.json());
      if (!parsed.success) return HttpResponse.json({ error: { code: 'VALIDATION_ERROR', message: 'Проверьте вопрос', trace_id: 'mock' } }, { status: 422 });
      const input = parsed.data, history = histories.get(id) ?? [];
      const existing = history.find(m => m.client_message_id === input.client_message_id);
      if (existing) return HttpResponse.json({ error: { code: existing.text === input.text ? 'MESSAGE_ALREADY_EXISTS' : 'IDEMPOTENCY_CONFLICT', message: 'Сообщение уже сохранено', trace_id: 'mock' } }, { status: 409 });
      const userId = `user_${input.client_message_id}`, assistantId = `assistant_${input.client_message_id}`;
      const user = messageSchema.parse({ ...fixture.messages[0], id: userId, text: input.text, client_message_id: input.client_message_id });
      const assistant = messageSchema.parse({ ...fixture.messages[1], id: assistantId, in_reply_to_message_id: userId, request_ids: [] });
      histories.set(id, [...history, user, assistant]);
      const events = [
        { event: 'message.started', data: { user_message_id: userId, message_id: assistantId } },
        { event: 'tool.started', data: { message_id: assistantId, tool_call_id: 'lookup', label: 'Ищу документы' } },
        { event: 'tool.finished', data: { message_id: assistantId, tool_call_id: 'lookup', status: 'succeeded' } },
        { event: 'answer.delta', data: { message_id: assistantId, text: assistant.text } },
        { event: 'message.completed', data: { message: assistant } },
      ];
      return new HttpResponse(events.map(e => `event: ${e.event}\ndata: ${JSON.stringify(e.data)}\n\n`).join(''), { headers: { 'Content-Type': 'text/event-stream' } });
    }),
  ];
}

export const notFound = () => HttpResponse.json({ error: {
  code: 'NOT_FOUND', message: 'Объект недоступен', trace_id: 'trace_mock',
} }, { status: 404 });
