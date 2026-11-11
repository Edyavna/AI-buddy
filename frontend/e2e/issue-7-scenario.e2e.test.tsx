import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createMemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { App, queryClient } from '@/app/App';
import { routes } from '@/app/router';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { userSchema } from '@/features/session/schemas';

// Сквозной сценарий приемки (docs/frontend/acceptance.md §«Сквозной тест»):
// вход → план → чат → черновик без внешнего номера → ровно один confirm →
// сохранение после reload → granted и обновление плана → завершение шага.
// Имитация stateful: состояние живет в MSW-хендлерах, reload переживает remount.
const f = createScenario();

let confirms = 0;
let sends = 0;

function countCalls() {
  const listener = ({ request }: { request: Request }) => {
    const url = new URL(request.url);
    if (request.method === 'POST' && /\/access-requests\/[^/]+\/confirm$/.test(url.pathname)) confirms += 1;
    if (request.method === 'POST' && /\/conversations\/[^/]+\/messages$/.test(url.pathname)) sends += 1;
  };
  server.events.on('request:match', listener);
  return () => server.events.removeListener('request:match', listener);
}

beforeEach(() => {
  queryClient.clear();
  confirms = 0;
  sends = 0;
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

it('проходит полный путь от входа до завершения шага без внешней заявки до подтверждения', async () => {
  const stopCounting = countCalls();
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

    // 3. Отправить вопрос: ответ со статусом инструмента и источником догружаются
    // (мгновенная имитация отдаёт весь поток разом; transient-статус инструмента
    // покрыт stream-тестами контроллера).
    fireEvent.change(screen.getByLabelText('Ваш вопрос'), { target: { value: 'Как запустить проект?' } });
    fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
    const sourceButton = await screen.findByRole('button', { name: new RegExp('Тестовая инструкция запуска') });
    expect(await screen.findByText(f.messages[1]!.text)).toBeInTheDocument();

    // 4. Открыть источник и вернуться в чат без потери сообщения.
    fireEvent.click(sourceButton);
    expect(await screen.findByText(/Синтетическая инструкция/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Закрыть панель' }));
    expect(screen.getByText(f.messages[1]!.text)).toBeInTheDocument();

    // 5. Открыть существующий диалог из сайдбара: карточка черновика с ID заявки,
    //    внешней заявки еще нет — внешний номер не показан, confirm не вызывался.
    const sidebar = screen.getByLabelText('Диалоги');
    const existingLink = within(sidebar).getAllByRole('link')
      .find(link => link.getAttribute('href') === `/chat/${f.conversation.id}`)!;
    fireEvent.click(existingLink);
    const chat = await screen.findByLabelText('Чат с помощником');
    const card = await within(chat).findByText(f.draft.resource.name);
    expect(card.closest('article')).toHaveAttribute('data-request-id', f.draft.id);
    expect(within(chat).getByRole('button', { name: 'Подтвердить отправку' })).toBeInTheDocument();
    expect(within(chat).queryByText(/Номер: /)).not.toBeInTheDocument();
    expect(confirms).toBe(0);

    // 6. Подтвердить: ровно одно создание, показ внешнего номера после сверки.
    // Фоновая сверка управляется таймерами — включаем управляемое время.
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.click(within(chat).getByRole('button', { name: 'Подтвердить отправку' }));
    expect(await within(chat).findByText('Отправляем заявку / уточняем результат')).toBeInTheDocument();
    expect(confirms).toBe(1);

    // 7. Фоновая сверка: submitting → pending (номер появился) → granted.
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(await within(chat).findByText(`Номер: ${f.pending.external_id}`)).toBeInTheDocument();
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(await within(chat).findByText('Доступ выдан')).toBeInTheDocument();

    // 8. Перезагрузить страницу: номер и статус сохранились, повторной отправки нет.
    view.unmount();
    const reloadRouter = createMemoryRouter(routes, { initialEntries: [`/chat/${f.conversation.id}`] });
    render(<App routerOverride={reloadRouter} />);
    const reloadedChat = await screen.findByLabelText('Чат с помощником');
    expect(await within(reloadedChat).findByText(`Номер: ${f.granted.external_id}`)).toBeInTheDocument();
    expect(within(reloadedChat).getByText('Доступ выдан')).toBeInTheDocument();
    await waitFor(() => expect(confirms).toBe(1));
    expect(sends).toBe(1);

    // 9. granted вернул шаг в работу: отметить выполненным, прогресс пересчитан сервером.
    await act(async () => { await reloadRouter.navigate('/onboarding'); });
    expect(await screen.findByRole('button', { name: 'Отметить выполненным' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Отметить выполненным' }));
    expect(await screen.findByText(/Завершено 1 из 1 шагов\./)).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: 'Прогресс плана адаптации' })).toHaveAttribute('aria-valuenow', '100');
  } finally {
    stopCounting();
  }
});
