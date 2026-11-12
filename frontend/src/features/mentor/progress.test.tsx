import { expect, it, vi, afterEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { http, HttpResponse, delay } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { routes } from '@/app/router';
import { createMentorMock, type MentorScenario } from '@/mocks/mentor/handlers';
import { createMentorFixtures } from '@/mocks/mentor/fixtures';
import { mentorKeys } from './api';
import { mentorApi } from './api';
import { focusManager } from '@tanstack/react-query';

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); focusManager.setFocused(undefined); });
function start(path = '/mentor/interns', scenario: MentorScenario = 'normal') {
  const mock = createMentorMock(scenario); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  return { ...renderWithQuery(<RouterProvider router={router} />), router };
}
it('uses server progress counts and opens a read-only plan without mutation controls', async () => {
  start();
  expect(await screen.findByText('Прогресс: 0% · 0 из 1 шагов')).toBeInTheDocument();
  expect(screen.getByText('Блокировки: 0 · Открытые вопросы: 1')).toBeInTheDocument();
  const link = screen.getByRole('link', { name: 'Тестовый стажер' }); fireEvent.click(link);
  expect(await screen.findByText('Подготовка к первой задаче')).toBeInTheDocument();
  expect(screen.getByText('План доступен только для просмотра. Стажёр сам отмечает выполнение шагов.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /выполн|начать|спросить/i })).not.toBeInTheDocument();
});
it('opens a direct assigned plan URL and displays blocked checkpoints', async () => {
  start('/mentor/interns/intern_demo_1');
  const f = createMentorFixtures(); f.plan.steps[0]!.status = 'blocked'; f.plan.steps[0]!.blocked_reason = 'Нужен доступ';
  server.use(http.get('*/api/v1/mentor/interns/:id/plan', () => HttpResponse.json(f.plan)));
  expect(await screen.findByText('Причина блокировки: Нужен доступ')).toBeInTheDocument();
  expect(screen.getByText('Заблокирован')).toBeInTheDocument();
});
it('shows empty assignments', async () => {
  start('/mentor/interns', 'empty');
  expect(await screen.findByText('Стажёры пока не назначены')).toBeInTheDocument();
});
it('does not expose another intern through a direct URL', async () => {
  start('/mentor/interns/foreign');
  expect(await screen.findByRole('alert')).toHaveTextContent(/недоступ/);
  expect(screen.queryByText('Подготовка к первой задаче')).not.toBeInTheDocument();
});
it.each([401, 403, 404])('hides previously loaded progress and plan after %s', async status => {
  const { queryClient, router } = start();
  await screen.findByText('Прогресс: 0% · 0 из 1 шагов');
  server.use(http.get('*/api/v1/mentor/interns', () => new HttpResponse(null, { status })));
  await queryClient.invalidateQueries({ queryKey: mentorKeys.interns });
  expect(await screen.findByRole('alert')).toHaveTextContent('Данные недоступны');
  expect(screen.queryByText('Тестовый стажер')).not.toBeInTheDocument();
  await router.navigate('/mentor/interns/intern_demo_1');
  await screen.findByText('Подготовка к первой задаче');
  server.use(http.get('*/api/v1/mentor/interns/:id/plan', () => new HttpResponse(null, { status })));
  await queryClient.invalidateQueries({ queryKey: mentorKeys.plan('intern_demo_1') });
  expect(await screen.findByRole('alert')).toHaveTextContent(/недоступ/);
  expect(screen.queryByText('Подготовка к первой задаче')).not.toBeInTheDocument();
});
it('offers retry on 503 without hiding last successful progress', async () => {
  const { queryClient } = start();
  await screen.findByText('Прогресс: 0% · 0 из 1 шагов');
  server.use(http.get('*/api/v1/mentor/interns', () => new HttpResponse(null, { status: 503 })));
  await queryClient.invalidateQueries({ queryKey: mentorKeys.interns });
  expect(await screen.findByRole('alert')).toHaveTextContent('503');
  expect(screen.getByText('Прогресс: 0% · 0 из 1 шагов')).toBeInTheDocument();
});
it('cancels progress requests when leaving the screen', async () => {
  const { router, queryClient } = start();
  await screen.findByText('Прогресс: 0% · 0 из 1 шагов');
  let aborted = false, entered = false;
  server.use(http.get('*/api/v1/mentor/interns', async ({ request }) => { entered = true; request.signal.addEventListener('abort', () => { aborted = true; }); await delay(200); return HttpResponse.json({ items: [] }); }));
  const update = queryClient.invalidateQueries({ queryKey: mentorKeys.interns });
  await waitFor(() => expect(queryClient.isFetching({ queryKey: mentorKeys.interns })).toBe(1));
  await waitFor(() => expect(entered).toBe(true));
  await router.navigate('/mentor/questions');
  await update;
  await waitFor(() => expect(aborted).toBe(true));
});
it('polls active screens every five seconds and pauses when hidden', async () => {
  const spy = vi.spyOn(mentorApi, 'interns');
  start(); await screen.findByText('Прогресс: 0% · 0 из 1 шагов');
  const initial = spy.mock.calls.length;
  // Advance only the polling timer; HTTP/React scheduling retains real timers.
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
  // The active observer already owns a real timer, so trigger visibility to reschedule.
  focusManager.setFocused(false); focusManager.setFocused(true);
  await vi.advanceTimersByTimeAsync(5000);
  await waitFor(() => expect(spy.mock.calls.length).toBeGreaterThan(initial));
  focusManager.setFocused(false); const hidden = spy.mock.calls.length;
  await vi.advanceTimersByTimeAsync(15000); expect(spy.mock.calls.length).toBe(hidden);
  vi.useRealTimers();
});
