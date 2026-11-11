import { afterEach, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { createQueryClient } from '@/shared/api';
import { queryKeys } from '@/shared/api';
import { accessRequestSchema } from '@/features/access-requests';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { ChatController } from './controller';
import { eventText } from '@/test/chat-stream';

const f = createScenario();
afterEach(() => { sessionStorage.clear(); vi.useRealTimers(); });

it.each(['older', 'equal-conflict', 'foreign'])('reconciles action.proposed using the shared FE2 cache: %s', async mode => {
  const { controller, client } = setup(); controller.reset(f.conversation.id);
  const cached = accessRequestSchema.parse(mode === 'older' ? f.pending : { ...f.draft, reason: 'Уже обновлённая причина' });
  client.setQueryData(queryKeys.request(f.draft.id), cached);
  server.use(http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([])),
    http.post('*/api/v1/conversations/:id/messages', () => new HttpResponse(
      eventText('message.started', { user_message_id: 'msg_u1', message_id: 'msg_a1' }) +
      eventText('action.proposed', { message_id: 'msg_a1', request: { ...f.draft, conversation_id: mode === 'foreign' ? 'other' : f.conversation.id } }) +
      eventText('message.completed', { message: f.messages[1] }),
      { headers: { 'Content-Type': 'text/event-stream' } },
    )));
  await controller.send('Вопрос', f.user.id, null, vi.fn());
  expect(client.getQueryData(queryKeys.request(f.draft.id))).toEqual(cached);
  if (mode === 'equal-conflict') expect(client.getQueryState(queryKeys.request(f.draft.id))?.isInvalidated).toBe(true);
  if (mode === 'foreign') expect(controller.state.messages.at(-1)?.request_ids).toEqual([]);
  controller.stop(); client.clear();
});
function setup() {
  const client = createQueryClient(); client.setDefaultOptions({ queries: { retry: false } });
  const unauthorized = vi.fn(); const controller = new ChatController(client, unauthorized);
  return { controller, client, unauthorized };
}
it('creates on explicit action, retains stream on navigate, ignores concurrent send and replaces deltas', async () => {
  const { controller } = setup();
  const listener = vi.fn(); const unsub = controller.subscribe(listener);
  let posts = 0;
  server.use(http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([])),
    http.post('*/api/v1/conversations/:id/messages', async ({ request }) => {
      posts++; const body = await request.json() as { client_message_id: string };
      const user = { id: 'msg_u1', client_message_id: body.client_message_id };
      return new HttpResponse(eventText('message.started', { user_message_id: user.id, message_id: 'msg_a1' }) + eventText('answer.delta', { message_id: 'msg_a1', text: 'partial' }) + eventText('message.completed', { message: f.messages[1] }), { headers: { 'Content-Type': 'text/event-stream' } });
    }));
  const navigate = vi.fn();
  const sending = controller.send('Вопрос', f.user.id, null, navigate);
  await controller.send('Второй', f.user.id, null, navigate);
  await sending;
  expect(posts).toBe(1); expect(navigate).toHaveBeenCalledOnce();
  expect(controller.state.phase).toBe('idle'); expect(controller.state.messages.at(-1)?.text).toBe(f.messages[1]?.text);
  expect(sessionStorage.length).toBe(0); expect(listener).toHaveBeenCalled(); unsub(); controller.stop();
});
it('recovers EOF and 409 from saved history without another generation', async () => {
  for (const conflict of [false, true]) {
    const { controller } = setup(); controller.reset(f.conversation.id);
    let sent = false, posts = 0;
    server.use(http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json(sent ? f.messages : [])),
      http.post('*/api/v1/conversations/:id/messages', () => {
        sent = true; posts++;
        return conflict ? HttpResponse.json({ error: { code: 'MESSAGE_ALREADY_EXISTS', message: 'Exists', trace_id: 't' } }, { status: 409 }) : new HttpResponse(': heartbeat\n\n', { headers: { 'Content-Type': 'text/event-stream' } });
      }));
    await controller.send('Вопрос', f.user.id, null, vi.fn());
    expect(posts).toBe(1); expect(controller.state.messages).toEqual(f.messages); controller.stop();
  }
});
it('persists unknown conversation creation, restores same IDs, supports explicit retry', async () => {
  const { controller } = setup();
  server.use(http.post('*/api/v1/conversations', () => HttpResponse.error()));
  await controller.send('Вопрос', f.user.id, 'step_local_setup', vi.fn());
  expect(controller.state.phase).toBe('failed');
  const saved = controller.restore(f.user.id); expect(saved?.text).toBe('Вопрос');
  const again = setup().controller;
  expect(again.restore(f.user.id)).toEqual(saved);
  await again.retry(f.user.id, vi.fn()); expect(again.restore(f.user.id)).toEqual(saved);
  expect(again.restore('another_user')).toBeNull();
  controller.stop(); again.stop();
});
it('does not send blank/oversize messages or after stop; resets and reactivates', async () => {
  const { controller } = setup();
  await controller.send(' ', 'u', null, vi.fn()); await controller.send('a'.repeat(8001), 'u', null, vi.fn());
  controller.stop(); await controller.send('x', 'u', null, vi.fn());
  expect(controller.state.messages).toEqual([]);
  controller.activate(); controller.reset('id'); expect(controller.state.conversationId).toBe('id'); controller.stop();
});
it('handles 401 and never clears unrelated session storage', async () => {
  const { controller, unauthorized } = setup(); controller.reset(f.conversation.id);
  sessionStorage.setItem('unrelated', 'keep');
  server.use(http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json({ error: { code: 'UNAUTHENTICATED', message: 'Войдите', trace_id: 't' } }, { status: 401 })));
  await controller.send('Вопрос', f.user.id, null, vi.fn());
  expect(unauthorized).toHaveBeenCalledOnce(); expect(sessionStorage.getItem('unrelated')).toBe('keep');
});
it('polls generating every 2s then displays interrupted history', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
  const { controller } = setup(); controller.reset(f.conversation.id);
  let calls = 0;
  server.use(http.get('*/api/v1/conversations/:id/messages', () => {
    calls++;
    return HttpResponse.json([f.messages[0], calls === 1 ? { ...f.messages[1], status: 'generating' } : { ...f.failed_event.data.message, status: 'interrupted', text: 'Частичный ответ' }]);
  }));
  const recovery = controller.recover(f.conversation.id);
  await vi.waitFor(() => expect(calls).toBe(1));
  await vi.advanceTimersByTimeAsync(2000); await recovery;
  expect(controller.state.phase).toBe('failed'); expect(controller.state.messages.at(-1)?.text).toBe(f.messages[1]?.text); expect(controller.state.messages.at(-1)?.status).toBe('interrupted'); controller.stop();
});

it('limits recovery to 130 seconds and allows cancellation during polling', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
  const { controller } = setup(); controller.reset(f.conversation.id);
  server.use(http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([f.messages[0], { ...f.messages[1], status: 'generating' }])));
  const recovering = controller.recover(f.conversation.id);
  await vi.waitFor(() => expect(controller.state.messages.length).toBe(2));
  await vi.advanceTimersByTimeAsync(132000); await recovering;
  expect(controller.state.phase).toBe('failed'); expect(controller.state.error).toContain('Обновите историю');
  controller.reset(f.conversation.id);
  const cancelled = controller.recover(f.conversation.id);
  await vi.waitFor(() => expect(controller.state.messages.length).toBe(2));
  controller.stop(); await cancelled;
});
