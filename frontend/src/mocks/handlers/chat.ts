import { http, HttpResponse } from 'msw';
import { z } from 'zod';
import { createScenario } from '../fixtures/scenario';
import type { Conversation } from '../../shared/api';

const createConversationSchema = z.object({
  client_conversation_id: z.uuid(), step_id: z.string().nullable().optional(),
}).strict();

// State is scoped to the handler factory/test. No random transitions or timers.
export function createChatHandlers() {
  const fixture = createScenario();
  const conversations: Conversation[] = [fixture.conversation];
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
      return HttpResponse.json(params.id === fixture.conversation.id ? fixture.messages : []);
    }),
  ];
}

export const notFound = () => HttpResponse.json({ error: {
  code: 'NOT_FOUND', message: 'Объект недоступен', trace_id: 'trace_mock',
} }, { status: 404 });
