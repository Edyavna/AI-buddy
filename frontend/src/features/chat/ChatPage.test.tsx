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

it.each([
  ['intern', 403], ['mentor', 403], ['intern', 404], ['mentor', 404],
] as const)('sends the first question after retrying a denied chat list: %s / %s', async (role, status) => {
  let allowed = false;
  const create = vi.fn(() => HttpResponse.json({ ...f.conversation, step_id: null }));
  const history = vi.fn(() => HttpResponse.json([]));
  const send = vi.fn(async ({ request }: { request: Request }) => {
    expect(await request.json()).toMatchObject({ text: 'Первый вопрос после повтора' });
    return new HttpResponse(
      eventText('message.started', { user_message_id: 'msg_u1', message_id: 'msg_a1' }) +
      eventText('message.completed', { message: f.messages[1] }),
      { headers: { 'Content-Type': 'text/event-stream' } },
    );
  });
  server.use(
    http.get('*/api/v1/me', () => HttpResponse.json({ ...f.user, role })),
    http.get('*/api/v1/conversations', () => allowed ? HttpResponse.json([]) : new HttpResponse(null, { status })),
    http.post('*/api/v1/conversations', create),
    http.get('*/api/v1/conversations/:id/messages', history),
    http.post('*/api/v1/conversations/:id/messages', send),
  );
  const path = role === 'mentor' ? '/mentor/chat' : '/chat';
  renderWithQuery(<MemoryRouter initialEntries={[path]}><Routes>
    <Route path={`${path}/:conversationId?`} element={<ChatPage mentor={role === 'mentor'} />} />
  </Routes></MemoryRouter>);
  await screen.findByRole('button', { name: 'Повторить' });
  expect(create).not.toHaveBeenCalled();
  expect(history).not.toHaveBeenCalled();
  allowed = true;
  fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
  await screen.findByText('Диалогов пока нет');
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(history).not.toHaveBeenCalled();
  fireEvent.change(screen.getByRole('textbox', { name: 'Ваш вопрос' }), { target: { value: 'Первый вопрос после повтора' } });
  fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
  await screen.findByText(f.messages[1]!.text);
  expect(create).toHaveBeenCalledTimes(1);
  expect(send).toHaveBeenCalledTimes(1);
});

it.each([['hr', 'hr'], ['mentor', 'mentor'], ['unknown', 'mentor']])('preserves a valid help recipient through conversation creation: %s', async (direction, expected) => {
  server.use(
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([])),
    http.post('*/api/v1/conversations/:id/messages', () => new HttpResponse(
      eventText('message.started', { user_message_id: 'msg_u1', message_id: 'msg_a1' }) +
      eventText('message.completed', { message: { ...f.messages[1], outcome: 'answered' } }),
      { headers: { 'Content-Type': 'text/event-stream' } },
    )),
    http.get('*/api/v1/messages/:id/questions', () => HttpResponse.json({
      items: [], drafts: [], question: 'Помощь по шагу', available_directions: ['mentor', 'hr'],
      routing: { direction: 'mentor', reason: 'Технический вопрос', clarification: null, split: false },
    })),
  );
  mount(`/chat?stepId=step_local_setup&direction=${direction}`);
  await waitFor(() => expect(screen.getByLabelText('Ваш вопрос')).toHaveValue(`Помоги выполнить шаг: ${f.plan.steps[0]?.title}`));
  fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Обращения' }));
  expect(await screen.findByRole('combobox', { name: /Адресат/ })).toHaveValue(expected);
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

it('archives and restores a conversation while keeping its history accessible', async () => {
  let archived = false;
  server.use(
    http.get('*/api/v1/conversations', ({ request }) => {
      const archive = new URL(request.url).searchParams.get('archived') === 'true';
      return HttpResponse.json(archived === archive ? [{ ...f.conversation, archived }] : []);
    }),
    http.patch('*/api/v1/conversations/:id', async ({ request }) => {
      archived = (await request.json() as { archived: boolean }).archived;
      return HttpResponse.json({ ...f.conversation, archived });
    }),
  );
  mount('/chat/conversation_demo_1');
  fireEvent.click(await screen.findByRole('button', { name: /^Архивировать диалог:/ }));
  expect(await screen.findByText('Диалогов пока нет')).toBeVisible();
  expect(screen.getByText(f.messages[1]!.text)).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Архив' }));
  expect(await screen.findByRole('link', { name: f.conversation.title })).toBeVisible();
  fireEvent.click(await screen.findByRole('button', { name: /^Вернуть из архива:/ }));
  expect(await screen.findByText('Архив пуст')).toBeVisible();
});
