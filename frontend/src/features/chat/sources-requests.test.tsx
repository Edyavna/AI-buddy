import { afterEach, expect, it, vi } from 'vitest';
import { act, fireEvent, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { queryKeys } from '@/shared/api';
import { accessRequestSchema } from '@/features/access-requests';
import { eventText } from '@/test/chat-stream';
import { ChatPage } from './ChatPage';

const f = createScenario();
const source = f.messages.flatMap(m => m.sources)[0]!;
afterEach(() => vi.unstubAllGlobals());
function mount() {
  return renderWithQuery(<MemoryRouter initialEntries={['/chat/conversation_demo_1']}><Routes>
    <Route path="/chat/:conversationId" element={<ChatPage />} />
  </Routes></MemoryRouter>);
}

it('restores source and real FE2 request card from history after remount, preserving the question through the drawer', async () => {
  const confirm = vi.fn(() => HttpResponse.error());
  server.use(http.post('*/api/v1/access-requests/:id/confirm', confirm));
  const view = mount();
  const sourceButton = await screen.findByRole('button', { name: new RegExp(source.title) });
  expect(await screen.findByRole('button', { name: 'Подтвердить отправку' })).toBeInTheDocument();
  expect(screen.queryByText(/Номер:/)).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Ваш вопрос'), { target: { value: 'Неотправленное уточнение' } });
  sourceButton.focus(); fireEvent.click(sourceButton);
  expect(await screen.findByText(/Синтетическая инструкция/)).toBeInTheDocument();
  fireEvent.keyDown(document, { key: 'Escape' });
  expect(sourceButton).toHaveFocus();
  expect(screen.getByLabelText('Ваш вопрос')).toHaveValue('Неотправленное уточнение');
  expect(confirm).not.toHaveBeenCalled();
  view.unmount();
  mount();
  expect(await screen.findByRole('button', { name: new RegExp(source.title) })).toBeInTheDocument();
  expect(await screen.findByRole('button', { name: 'Подтвердить отправку' })).toBeInTheDocument();
});

it('shows document 404 inside the drawer without losing chat history', async () => {
  server.use(http.get('*/api/v1/documents/:id', () => HttpResponse.json({ error: { code: 'NOT_FOUND', message: 'Нет доступа', trace_id: 't' } }, { status: 404 })));
  mount();
  fireEvent.click(await screen.findByRole('button', { name: new RegExp(source.title) }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Документ недоступен или удалён');
  fireEvent.click(screen.getByRole('button', { name: 'Закрыть панель' }));
  expect(screen.getByText(f.messages[1]!.text)).toBeInTheDocument();
});

it('deduplicates request IDs and onDiscuss focuses the composer without sending', async () => {
  const send = vi.fn(() => HttpResponse.error());
  server.use(http.get('*/api/v1/access-requests/:id', () => HttpResponse.json({ ...f.pending, status: 'rejected', failure_reason: 'Не согласовано' })),
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([f.messages[0], { ...f.messages[1], request_ids: [f.draft.id, f.draft.id] }])),
    http.post('*/api/v1/conversations/:id/messages', send));
  mount();
  const discuss = await screen.findByRole('button', { name: 'Обсудить с помощником' });
  expect(screen.getAllByText(f.draft.resource.name)).toHaveLength(1);
  fireEvent.click(discuss);
  expect(screen.getByLabelText('Ваш вопрос')).toHaveFocus(); expect(send).not.toHaveBeenCalled();
});

it('keeps history scroll position when reading above and scrolls only on explicit new-message action', async () => {
  let resized = () => {};
  const disconnected = vi.fn();
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: () => void) { resized = callback; }
    observe() {}
    disconnect = disconnected;
  });
  const view = mount();
  await screen.findByText(f.messages[1]!.text);
  const scroller = screen.getByRole('log', { name: 'История сообщений' });
  Object.defineProperties(scroller, { scrollHeight: { value: 1000, configurable: true }, clientHeight: { value: 200, configurable: true } });
  act(() => resized());
  expect(scroller.scrollTop).toBe(1000);
  scroller.scrollTop = 100;
  fireEvent.scroll(scroller);
  const original = scroller.scrollTop;
  act(() => resized());
  expect(scroller.scrollTop).toBe(original);
  fireEvent.click(screen.getByRole('button', { name: 'Новые сообщения' }));
  expect(scroller.scrollTop).toBe(1000);
  scroller.scrollTop = original;
  fireEvent.scroll(scroller);
  fireEvent.click(await screen.findByRole('button', { name: new RegExp(source.title) }));
  await screen.findByText(/Синтетическая инструкция/);
  fireEvent.keyDown(document, { key: 'Escape' });
  expect(scroller.scrollTop).toBe(original);
  view.unmount();
  expect(disconnected).toHaveBeenCalled();
});

it('accepts an SSE draft before the final message and confirms using the real FE2 panel', async () => {
  let finish = () => {};
  let request = accessRequestSchema.parse(f.draft);
  let confirms = 0;
  server.use(
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([])),
    http.get('*/api/v1/access-requests/:id', () => HttpResponse.json(request)),
    http.post('*/api/v1/access-requests/:id/confirm', async ({ request: incoming }) => {
      expect(await incoming.json()).toEqual({ draft_version: f.draft.draft_version });
      confirms++; request = accessRequestSchema.parse(f.pending); return HttpResponse.json(request);
    }),
    http.post('*/api/v1/conversations/:id/messages', () => new HttpResponse(new ReadableStream<Uint8Array>({ start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode(eventText('message.started', { user_message_id: 'msg_u1', message_id: 'msg_a1' }) + eventText('action.proposed', { message_id: 'msg_a1', request })));
      finish = () => { controller.enqueue(encoder.encode(eventText('message.completed', { message: f.messages[1] }))); controller.close(); };
    } }), { headers: { 'Content-Type': 'text/event-stream' } })),
  );
  const { queryClient } = mount();
  await screen.findByText('Шаг: Запустить проект');
  fireEvent.change(screen.getByLabelText('Ваш вопрос'), { target: { value: 'Нужен доступ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
  const button = await screen.findByRole('button', { name: 'Подтвердить отправку' });
  expect(queryClient.getQueryData(queryKeys.request(f.draft.id))).toMatchObject({ status: 'draft', revision: f.draft.revision });
  expect(confirms).toBe(0);
  fireEvent.click(button);
  expect(await screen.findByText(`Номер: ${f.pending.external_id}`)).toBeInTheDocument();
  expect(confirms).toBe(1);
  finish();
  expect(await screen.findByText(f.messages[1]!.text)).toBeInTheDocument();
  expect(within(screen.getByLabelText('Чат с помощником')).queryByRole('button', { name: 'Подтвердить отправку' })).not.toBeInTheDocument();
});
