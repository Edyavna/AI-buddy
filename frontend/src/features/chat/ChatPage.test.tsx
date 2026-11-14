import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { eventText } from '@/test/chat-stream';
import { queryKeys } from '@/shared/api';
import { planSchema, type Plan } from '@/features/onboarding';
import { ChatPage } from './ChatPage';

const f = createScenario();
function mount(url = '/chat', plan?: Plan) {
  server.use(http.get('*/api/v1/me', () => HttpResponse.json(f.user)), http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(plan ?? f.plan)));
  return renderWithQuery(<MemoryRouter initialEntries={[url]}><Routes><Route path="/chat" element={<ChatPage />} /><Route path="/chat/:conversationId" element={<ChatPage />} /><Route path="/login" element={<p>Вход</p>} /></Routes></MemoryRouter>);
}
it('prefills verified step without creating anything and rejects unknown step', async () => {
  const create = vi.fn(() => HttpResponse.error());
  server.use(http.post('*/api/v1/conversations', create));
  const view = mount('/chat?stepId=step_local_setup');
  await waitFor(() => expect(screen.getByLabelText('Ваш вопрос')).toHaveValue(`Помоги выполнить шаг: ${f.plan.steps[0]?.title}`));
  expect(create).not.toHaveBeenCalled(); view.unmount();
  mount('/chat?stepId=unknown');
  expect(await screen.findByText(/Шаг не найден/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Отправить' })).toBeDisabled();
});
it('keeps checkpoint fields in the shared plan cache', async () => {
  const plan = planSchema.parse(f.plan);
  const step = plan.steps[0]!;
  step.revision = 7;
  step.allowed_actions = ['revoke'];
  const { queryClient } = mount('/chat?stepId=step_local_setup', plan);

  await screen.findByText(`Шаг: ${step.title}`);
  expect(queryClient.getQueryData(queryKeys.plan())).toMatchObject({
    steps: [{ revision: 7, allowed_actions: ['revoke'] }],
  });
});

it('restores direct URL history and context', async () => {
  mount('/chat/conversation_demo_1');
  expect(await screen.findByText(f.messages[1]!.text)).toBeInTheDocument();
  expect(await screen.findByText(`Шаг: ${f.plan.steps[0]?.title}`)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: f.conversation.title })).toHaveAttribute('aria-current', 'page');
});
it('opens a cited message once after loading history and preserves its scroll position', async () => {
  const scroll = vi.fn(function(this: HTMLElement) {
    this.closest('[role="log"]')!.scrollTop = 37;
  });
  const previous = HTMLElement.prototype.scrollIntoView;
  HTMLElement.prototype.scrollIntoView = scroll;
  try {
    const view = mount('/chat/conversation_demo_1#message-' + f.messages[0]!.id);
    await waitFor(() => expect(scroll).toHaveBeenCalledTimes(1));
    expect(scroll.mock.instances[0]).toHaveAttribute('id', 'message-' + f.messages[0]!.id);
    expect(screen.getByRole('log').scrollTop).toBe(37);
    await view.queryClient.invalidateQueries({ queryKey: queryKeys.messages('conversation_demo_1') });
    expect(scroll).toHaveBeenCalledTimes(1);
    view.unmount();
    mount('/chat/conversation_demo_1#message-%E0%A4%A');
    await screen.findByText(f.messages[1]!.text);
    expect(scroll).toHaveBeenCalledTimes(1);
  } finally { HTMLElement.prototype.scrollIntoView = previous; }
});
it('sends with Enter, does not send with Shift or IME, and survives internal URL replace', async () => {
  let posts = 0;
  server.use(http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([])),
    http.post('*/api/v1/conversations/:id/messages', () => {
      posts++;
      return new HttpResponse(eventText('message.started', { user_message_id: 'msg_u1', message_id: 'msg_a1' }) + eventText('answer.delta', { message_id: 'msg_a1', text: 'Черновой' }) + eventText('message.completed', { message: f.messages[1] }), { headers: { 'Content-Type': 'text/event-stream' } });
    }));
  mount();
  await waitFor(() => expect(screen.getByRole('link', { name: f.conversation.title })).toBeInTheDocument());
  const input = screen.getByLabelText('Ваш вопрос');
  fireEvent.change(input, { target: { value: 'Вопрос' } });
  fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });
  fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
  expect(posts).toBe(0);
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(await screen.findByText(f.messages[1]!.text)).toBeInTheDocument(); expect(posts).toBe(1);
});
it('offers escalation only for a completed knowledge gap and repeats with a fresh message ID', async () => {
  const original = f.messages[0]!;
  const gap = { ...f.messages[1]!, outcome: 'knowledge_gap', sources: [], request_ids: [] };
  const answered = { ...gap, id: 'answered', outcome: 'answered' };
  const clarification = { ...gap, id: 'clarification', outcome: 'needs_clarification' };
  let repeated: { client_message_id: string; text: string } | undefined;
  const status = vi.fn(() => HttpResponse.json({
    question: {
      id: 'f47e17f0-47d4-401c-92f8-303cc7f676a8', message_id: gap.id, status: 'resolved', revision: 3,
      resolution: 'Инструкция готова', document_ids: ['doc_local_setup'], created_at: gap.created_at, updated_at: gap.created_at,
    },
  }));
  server.use(
    http.get('*/api/v1/messages/:id/knowledge-gap', status),
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([original, gap, answered, clarification])),
    http.post('*/api/v1/conversations/:id/messages', async ({ request }) => {
      repeated = await request.json() as typeof repeated;
      return new HttpResponse(eventText('message.started', { user_message_id: 'repeated', message_id: 'follow-up' }) +
        eventText('message.completed', { message: { ...answered, id: 'follow-up', in_reply_to_message_id: 'repeated', text: 'Нашёл инструкцию' } }),
      { headers: { 'Content-Type': 'text/event-stream' } });
    }),
  );
  mount('/chat/conversation_demo_1');
  fireEvent.click(await screen.findByRole('button', { name: 'Повторить вопрос' }));
  expect(await screen.findByText('Нашёл инструкцию')).toBeInTheDocument();
  expect(status.mock.calls.length).toBeGreaterThan(0);
  expect(screen.getAllByRole('region', { name: 'Вопрос наставнику' })).toHaveLength(1);
  expect(repeated?.text).toBe(original.text);
  expect(repeated?.client_message_id).toMatch(/^[0-9a-f-]{36}$/);
  expect(repeated?.client_message_id).not.toBe(original.client_message_id);
});
it('redirects 401 and displays missing conversation without sending', async () => {
  const view = mount('/chat/missing');
  expect(await screen.findByRole('alert')).toHaveTextContent(/Объект/);
  view.unmount();
  server.use(http.get('*/api/v1/me', () => HttpResponse.json({ error: { code: 'UNAUTHENTICATED', message: 'Войдите', trace_id: 't' } }, { status: 401 })));
  renderWithQuery(<MemoryRouter initialEntries={['/chat']}><Routes><Route path="/chat" element={<ChatPage />} /><Route path="/login" element={<p>Вход</p>} /></Routes></MemoryRouter>);
  expect(await screen.findByText('Вход')).toBeInTheDocument();
});
