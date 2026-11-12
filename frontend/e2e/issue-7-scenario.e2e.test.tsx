import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createMemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { App, queryClient } from '@/app/App';
import { routes } from '@/app/router';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { userSchema } from '@/features/session/schemas';
import { messageSchema } from '@/features/chat/schemas';
import { accessRequestSchema } from '@/features/access-requests/schemas';
import { createHandlers } from '@/mocks/handlers';
import { createFe2Handlers } from '@/mocks/handlers/fe2';
import { planSchema } from '@/features/onboarding';
import { queryKeys } from '@/shared/api';

// Сквозной сценарий приемки (docs/frontend/acceptance.md §«Сквозной тест»):
// вход → план → чат → черновик ИЗ НОВОГО ВОПРОСА → подтверждение →
// reload с пустым кешем → granted и обновление плана → завершение шага.
// Имитация stateful: состояние живет в MSW-хендлерах, reload переживает remount.
const f = createScenario();

let confirms = 0;
let sends = 0;
let requestGets = 0;

function countCalls() {
  const listener = ({ request }: { request: Request }) => {
    const url = new URL(request.url);
    if (request.method === 'POST' && /\/access-requests\/[^/]+\/confirm$/.test(url.pathname)) confirms += 1;
    if (request.method === 'POST' && /\/conversations\/[^/]+\/messages$/.test(url.pathname)) sends += 1;
    if (request.method === 'GET' && /\/access-requests\/[^/]+$/.test(url.pathname)) requestGets += 1;
  };
  server.events.on('request:match', listener);
  return () => server.events.removeListener('request:match', listener);
}

beforeEach(() => {
  // This acceptance path completes its single access-dependent step. The full
  // checklist has separate policy tests and must not silently change this fixture.
  server.resetHandlers(...createHandlers(createFe2Handlers({ plan: planSchema.parse(f.plan) })));
  queryClient.clear();
  confirms = 0;
  sends = 0;
  requestGets = 0;
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

it('проходит полный путь от входа до завершения шага: черновик из нового вопроса, reload с пустым кешем', async () => {
  const stopCounting = countCalls();
  let finishAnswer: (() => void) | undefined;
  // История диалога, созданного в этом тесте, живёт здесь: дефолтный чат-мок о ней
  // не знает, поэтому GET messages для нового диалога обслуживаем этой же картой.
  const streamedHistories = new Map<string, unknown[]>();
  // Имитация агента: ответ на новый вопрос предлагает черновик (draft.saved →
  // action.proposed) ещё до финального сообщения; внешней заявки при этом нет.
  server.use(
    http.post('*/api/v1/conversations/:id/messages', async ({ params, request }) => {
      const id = String(params.id);
      const body = (await request.json()) as { client_message_id: string; text: string };
      const userId = `user_${body.client_message_id}`, assistantId = `assistant_${body.client_message_id}`;
      const user = messageSchema.parse({ ...f.messages[0], id: userId, text: body.text, client_message_id: body.client_message_id });
      const draft = accessRequestSchema.parse({ ...f.draft, conversation_id: id });
      const assistant = messageSchema.parse({ ...f.messages[1], id: assistantId, in_reply_to_message_id: userId, request_ids: [draft.id] });
      streamedHistories.set(id, [user]);
      const events = [
        { event: 'message.started', data: { user_message_id: userId, message_id: assistantId } },
        { event: 'tool.started', data: { message_id: assistantId, tool_call_id: 'lookup', label: 'Ищу документы' } },
        { event: 'tool.finished', data: { message_id: assistantId, tool_call_id: 'lookup', status: 'succeeded' } },
        { event: 'action.proposed', data: { message_id: assistantId, request: draft } },
      ];
      const encoder = new TextEncoder();
      const encode = (event: { event: string; data: unknown }) =>
        encoder.encode(`event: ${event.event}\ndata: ${JSON.stringify(event.data)}\n\n`);
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          for (const event of events) controller.enqueue(encode(event));
          finishAnswer = () => {
            finishAnswer = undefined;
            streamedHistories.set(id, [user, assistant]);
            controller.enqueue(encode({ event: 'answer.delta', data: { message_id: assistantId, text: assistant.text } }));
            controller.enqueue(encode({ event: 'message.completed', data: { message: assistant } }));
            controller.close();
          };
        },
        cancel() { finishAnswer = undefined; },
      });
      return new HttpResponse(stream, { headers: { 'Content-Type': 'text/event-stream' } });
    }),
    http.get('*/api/v1/conversations/:id/messages', ({ params }) => {
      const id = String(params.id);
      if (streamedHistories.has(id)) return HttpResponse.json(streamedHistories.get(id));
      if (id === f.conversation.id) return HttpResponse.json(f.messages);
      // Новый диалог до первого ответа имеет пустую историю (сверка исхода отправки).
      return HttpResponse.json([]);
    }),
  );
  // Имитация сессии: до входа /me отвечает 401, затем отдаёт выбранного стажера.
  let sessionEstablished = false;
  server.use(
    http.get('*/api/v1/me', () =>
      sessionEstablished
        ? HttpResponse.json(userSchema.parse(f.user))
        : HttpResponse.json({ error: { code: 'UNAUTHORIZED', message: 'Сессия не найдена', trace_id: 'trace_e2e' } }, { status: 401 })),
  );
  try {
    const router = createMemoryRouter(routes, { initialEntries: ['/login'] });
    const view = render(<App routerOverride={router} />);

    // 1. Войти тестовым стажером, получить план.
    fireEvent.click(await screen.findByRole('button', { name: 'Войти как Тестовый стажер' }));
    sessionEstablished = true;
    expect(await screen.findByRole('heading', { name: f.plan.title })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Спросить помощника о шаге: Запустить проект' })).toBeInTheDocument();

    // 2. Нажать «Спросить помощника» у шага: контекст подставлен, автоотправки нет.
    fireEvent.click(screen.getByRole('link', { name: 'Спросить помощника о шаге: Запустить проект' }));
    expect(await screen.findByRole('heading', { name: 'Помощник стажёра' })).toBeInTheDocument();
    expect(screen.getByLabelText('Ваш вопрос')).toHaveValue('Помоги выполнить шаг: Запустить проект');
    expect(sends).toBe(0);

    // 3. Отправить вопрос: ответ со статусом инструмента и источником догружаются,
    //    а агент предлагает черновик — карточка с ID заявки появляется в новом диалоге.
    //    Внешней заявки ещё нет: номер не показан, confirm не вызывался.
    fireEvent.change(screen.getByLabelText('Ваш вопрос'), { target: { value: 'Как запустить проект?' } });
    fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
    const chat = screen.getByLabelText('Чат с помощником');
    const card = await within(chat).findByText(f.draft.resource.name);
    expect(card.closest('article')).toHaveAttribute('data-request-id', f.draft.id);
    expect(within(chat).getByRole('button', { name: 'Подтвердить отправку' })).toBeInTheDocument();
    expect(within(chat).queryByText(/Номер: /)).not.toBeInTheDocument();
    expect(confirms).toBe(0);

    // Финальный ответ ещё не отправлен: карточку могло дать только action.proposed.
    expect(within(chat).queryByText(f.messages[1]!.text)).not.toBeInTheDocument();
    expect(finishAnswer).toBeTypeOf('function');
    await act(async () => { finishAnswer!(); });
    expect(await within(chat).findByText(f.messages[1]!.text)).toBeInTheDocument();

    // 4. Открыть источник и вернуться в чат без потери сообщения.
    fireEvent.click(await screen.findByRole('button', { name: new RegExp('Тестовая инструкция запуска') }));
    expect(await screen.findByText(/Синтетическая инструкция/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Закрыть панель' }));
    expect(screen.getByText(f.messages[1]!.text)).toBeInTheDocument();

    // 5. Подтвердить: ровно одно создание, показ внешнего номера после сверки.
    //    Фоновая сверка управляется таймерами — включаем управляемое время.
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.click(within(chat).getByRole('button', { name: 'Подтвердить отправку' }));
    expect(await within(chat).findByText('Отправляем заявку / уточняем результат')).toBeInTheDocument();
    expect(confirms).toBe(1);

    // 6. Фоновая сверка: submitting → pending (номер появился) → granted.
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(await within(chat).findByText(`Номер: ${f.pending.external_id}`)).toBeInTheDocument();
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(await within(chat).findByText('Доступ выдан')).toBeInTheDocument();
    const conversationId = decodeURIComponent(router.state.location.pathname.replace('/chat/', ''));
    expect(streamedHistories.has(conversationId)).toBe(true);

    // 7. Перезагрузить страницу: кеш очищен, состояние должно прийти из API-мока.
    const getsBeforeReload = requestGets;
    view.unmount();
    queryClient.clear();
    const reloadRouter = createMemoryRouter(routes, { initialEntries: [`/chat/${conversationId}`] });
    render(<App routerOverride={reloadRouter} />);
    const reloadedChat = await screen.findByLabelText('Чат с помощником');
    expect(await within(reloadedChat).findByText(`Номер: ${f.granted.external_id}`)).toBeInTheDocument();
    expect(within(reloadedChat).getByText('Доступ выдан')).toBeInTheDocument();
    // Карточка восстановлена именно запросом к API, а не пережившим reload кешем.
    expect(requestGets).toBeGreaterThan(getsBeforeReload);
    await waitFor(() => expect(confirms).toBe(1));
    expect(sends).toBe(1);

    // 8. granted вернул шаг в работу: отметить выполненным, прогресс пересчитан сервером.
    await act(async () => { await reloadRouter.navigate('/onboarding'); });
    fireEvent.click(await screen.findByRole('button', { name: 'Подтвердить выполнение' }));
    expect(await screen.findByText(/Завершено 1 из 1 шагов\./)).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: 'Прогресс плана адаптации' })).toHaveAttribute('aria-valuenow', '100');

    const getsAfterCompletion = requestGets;
    await act(async () => {
      await queryClient.refetchQueries({ queryKey: queryKeys.request(f.granted.id) });
      await queryClient.refetchQueries({ queryKey: queryKeys.plan() });
    });
    expect(requestGets).toBeGreaterThan(getsAfterCompletion);
    expect(screen.getByRole('progressbar', { name: 'Прогресс плана адаптации' })).toHaveAttribute('aria-valuenow', '100');
  } finally {
    await act(async () => { finishAnswer?.(); });
    stopCounting();
  }
});
