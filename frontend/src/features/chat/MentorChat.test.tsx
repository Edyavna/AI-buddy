import { expect, it, vi } from 'vitest';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { createScenario } from '@/mocks/fixtures/scenario';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { ChatPage } from './ChatPage';
import { ChatController } from './controller';
import { queryKeys } from '@/shared/api';

const fixture = createScenario();

it.each([false, true])('keeps intern memory out of mentor chat, Markdown=%s', async markdown => {
  const memory = vi.fn(() => HttpResponse.json({ state: 'ready', items: [] }));
  server.use(
    http.get('*/api/v1/conversations/:id', () => HttpResponse.json({ ...fixture.conversation, step_id: null, markdown })),
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json(fixture.messages)),
    http.get('*/api/v1/messages/:id/memory-proposals', memory),
    http.post('*/api/v1/messages/:id/memory-proposals', memory),
  );
  mount(`/mentor/chat/${fixture.conversation.id}`);
  await screen.findByText(fixture.messages[1]!.text);
  expect(screen.queryByRole('link', { name: 'Моя память' })).not.toBeInTheDocument();
  expect(screen.queryByRole('region', { name: 'Предложения персональной памяти' })).not.toBeInTheDocument();
  expect(memory).not.toHaveBeenCalled();
});
function mount(path: string) {
  server.use(http.get('*/api/v1/me', () => HttpResponse.json({ ...fixture.user, role: 'mentor' })));
  return renderWithQuery(
    <MemoryRouter initialEntries={[path]}>
      <Routes><Route path="/mentor/chat/:conversationId?" element={<ChatPage mentor />} /></Routes>
    </MemoryRouter>,
  );
}

it('resends the original message after a saved model failure', async () => {
  const [question, answer] = fixture.messages;
  server.use(
    http.get('*/api/v1/conversations/:id', () => HttpResponse.json({ ...fixture.conversation, step_id: null })),
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([
      question,
      { ...answer, status: 'failed', error: { error: { code: 'LLM_UNAVAILABLE', message: 'Модель недоступна', trace_id: 'test' } } },
    ])),
  );
  const send = vi.spyOn(ChatController.prototype, 'send').mockResolvedValue(undefined);
  try {
    mount(`/mentor/chat/${fixture.conversation.id}`);
    fireEvent.click(await screen.findByRole('button', { name: 'Повторить запрос' }));
    expect(send).toHaveBeenCalledWith(question!.text, fixture.user.id, null, expect.any(Function), { newMessage: true });
  } finally {
    send.mockRestore();
  }
});

it('retries creation after the network request fails', async () => {
  let attempts = 0;
  server.use(http.post('*/api/v1/conversations', () => {
    attempts++;
    return HttpResponse.error();
  }));
  mount('/mentor/chat');
  await screen.findByRole('link', { name: fixture.conversation.title });
  fireEvent.change(screen.getByRole('textbox', { name: 'Ваш вопрос' }), { target: { value: 'Вопрос наставника' } });
  fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Повторить исходный вопрос' }));
  await screen.findByRole('button', { name: 'Повторить исходный вопрос' });
  await waitFor(() => expect(attempts).toBe(2));
  sessionStorage.clear();
});

it.each([403, 404])('removes previously loaded mentor history after access denial %s and a later 503', async status => {
  let response = 200;
  server.use(
    http.get('*/api/v1/conversations/:id', () => response === 200
      ? HttpResponse.json({ ...fixture.conversation, step_id: null }) : new HttpResponse(null, { status: response })),
    http.get('*/api/v1/conversations/:id/messages', () => response === 200
      ? HttpResponse.json(fixture.messages) : new HttpResponse(null, { status: 503 })),
  );
  const { queryClient } = mount(`/mentor/chat/${fixture.conversation.id}`);
  await screen.findByText(fixture.messages[1]!.text);
  response = status;
  await act(() => queryClient.refetchQueries({ queryKey: queryKeys.conversation(fixture.conversation.id) }));
  await waitFor(() => expect(screen.queryByText(fixture.messages[1]!.text)).not.toBeInTheDocument());
  expect(queryClient.getQueryData(queryKeys.messages(fixture.conversation.id))).toBeNull();
  response = 503;
  await act(() => queryClient.refetchQueries({ queryKey: ['chat'] }));
  expect(screen.queryByText(fixture.messages[1]!.text)).not.toBeInTheDocument();
});

it('discards a late history response after a denial and restores only a fresh authorized read', async () => {
  let status = 200;
  let delayed = false;
  let release!: () => void;
  server.use(
    http.get('*/api/v1/conversations/:id', () => status === 200 ? HttpResponse.json({ ...fixture.conversation, step_id: null }) : new HttpResponse(null, { status })),
    http.get('*/api/v1/conversations/:id/messages', async () => {
      if (delayed) await new Promise<void>(resolve => { release = resolve; });
      return HttpResponse.json(fixture.messages);
    }),
  );
  const { queryClient } = mount(`/mentor/chat/${fixture.conversation.id}`);
  await screen.findByText(fixture.messages[1]!.text);
  delayed = true;
  const stale = queryClient.refetchQueries({ queryKey: queryKeys.messages(fixture.conversation.id) });
  await waitFor(() => expect(release).toBeDefined());
  status = 403;
  await act(() => queryClient.refetchQueries({ queryKey: queryKeys.conversation(fixture.conversation.id) }));
  await act(async () => { release(); await stale; });
  expect(screen.queryByText(fixture.messages[1]!.text)).not.toBeInTheDocument();
  expect(queryClient.getQueryData(queryKeys.messages(fixture.conversation.id))).toBeNull();
  delayed = false; status = 200;
  await act(() => queryClient.refetchQueries({ queryKey: ['chat'] }));
  await screen.findByText(fixture.messages[1]!.text);
});

it('clears history after a denied archive mutation followed by a 503', async () => {
  let denied = false;
  server.use(
    http.get('*/api/v1/conversations/:id', () => denied ? new HttpResponse(null, { status: 503 }) : HttpResponse.json({ ...fixture.conversation, step_id: null })),
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json(fixture.messages)),
    http.patch('*/api/v1/conversations/:id', () => { denied = true; return new HttpResponse(null, { status: 403 }); }),
  );
  const { queryClient } = mount(`/mentor/chat/${fixture.conversation.id}`);
  await screen.findByText(fixture.messages[1]!.text);
  fireEvent.click(screen.getByRole('button', { name: `Архивировать диалог: ${fixture.conversation.title}` }));
  await waitFor(() => expect(screen.queryByText(fixture.messages[1]!.text)).not.toBeInTheDocument());
  await act(() => queryClient.refetchQueries({ queryKey: ['chat'] }));
  expect(queryClient.getQueryData(queryKeys.messages(fixture.conversation.id))).toBeNull();
  expect(screen.queryByText(fixture.messages[1]!.text)).not.toBeInTheDocument();
});
