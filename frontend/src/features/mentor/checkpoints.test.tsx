import { it, expect, vi, afterEach } from 'vitest';
import { act, screen, fireEvent, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { routes } from '@/app/router';
import { planSchema, type Plan, type CheckpointCompletionPolicy } from '@/features/onboarding';
import { resetSessionState } from '@/features/session';
import { createFullCheckpointPlan } from '@/mocks/fixtures/checkpoints';
import { createMentorFixtures } from '@/mocks/mentor/fixtures';
import { createMentorMock, type MentorScenario } from '@/mocks/mentor/handlers';
import { mentorApi, mentorKeys } from './api';
const id = 'intern_demo_1', stepId = 'step_local_setup';
function fixture(policy: CheckpointCompletionPolicy = 'both') {
  const base = createMentorFixtures().plan;
  return planSchema.parse({ ...base, steps: [{ ...base.steps[0]!, title: 'Контрольный шаг', status: 'not_started', completion_policy: policy, allowed_actions: [], request_ids: [], confirmations: [], instructions: '1. Первый подпункт\n2. Второй подпункт', period: '2 неделя', resources: [{ title: 'Материал без URL', url: null, note: null }, { title: 'Опасная ссылка', url: 'javascript:alert(1)', note: null }] }] });
}
function start(plan = fixture(), scenario: MentorScenario = 'normal') {
  const mock = createMentorMock(scenario, { plan }); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const router = createMemoryRouter(routes, { initialEntries: [`/mentor/interns/${id}`] });
  return { ...renderWithQuery(<RouterProvider router={router} />), mock, router };
}
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); });
it.each(['intern', 'mentor', 'both'] as const)('renders full instructions and server permissions for %s', async policy => {
  const plan = fixture(policy);
  plan.steps[0]!.description = plan.steps[0]!.instructions;
  start(plan);
  await screen.findByText('Контрольный шаг');
  expect(screen.getByText('Период: 2 неделя')).toBeInTheDocument();
  expect(screen.getByText('Первый подпункт')).toBeInTheDocument(); expect(screen.getByText('Второй подпункт')).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: 'Опасная ссылка' })).not.toBeInTheDocument();
  expect(screen.queryByRole('link', { name: 'Материал без URL' })).not.toBeInTheDocument();
  expect(screen.getAllByText('Уточнить у наставника')).toHaveLength(2);
  expect(screen.queryByRole('link', { name: /Спросить помощника/ })).not.toBeInTheDocument();
  if (policy === 'intern') expect(screen.queryByRole('button', { name: 'Подтвердить свою часть' })).not.toBeInTheDocument();
  else expect(screen.getByRole('button', { name: 'Подтвердить свою часть' })).toBeEnabled();
});
it('renders all 17 rows, including repeated catalog titles, without merging them', async () => {
  start(createFullCheckpointPlan());
  await screen.findByRole('heading', { name: 'Организационные моменты' });
  expect(document.querySelectorAll('[data-step-id]')).toHaveLength(17);
  expect(screen.getAllByRole('heading', { name: 'Экваторная встреча' })).toHaveLength(2);
});
it.each(['intern-first', 'mentor-first'])('handles both in order %s and revokes only the mentor mark', async order => {
  const { mock, queryClient, router } = start();
  if (order === 'intern-first') mock.control.confirmIntern(stepId, true);
  fireEvent.click(await screen.findByRole('button', { name: 'Подтвердить свою часть' }));
  if (order === 'mentor-first') {
    expect(await screen.findByText('Ожидаем подтверждение стажёра')).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: 'Прогресс плана' })).toHaveAttribute('value', '0');
    mock.control.confirmIntern(stepId, true);
    await queryClient.refetchQueries({ queryKey: mentorKeys.plan(id) });
    await queryClient.invalidateQueries({ queryKey: mentorKeys.interns });
  }
  await waitFor(() => expect(screen.getByRole('progressbar', { name: 'Прогресс плана' })).toHaveAttribute('value', '100'));
  expect(await screen.findByText('Завершено 1 из 1 шагов · Блокировки: 0')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Отозвать свою отметку' }));
  expect(await screen.findByText('Ожидаем подтверждение наставника')).toBeInTheDocument();
  expect(mock.control.plan.steps[0]?.confirmations.map(c => c.role)).toEqual(['intern']);
  await router.navigate('/mentor/interns');
  expect(await screen.findByText('Прогресс: 0% · 0 из 1 шагов')).toBeInTheDocument();
});
it('completes a mentor-only step and respects blocked allowed_actions', async () => {
  const plan = fixture('mentor'); plan.steps[0]!.status = 'blocked'; plan.steps[0]!.blocked_reason = 'Не завершён предыдущий шаг';
  const { queryClient, mock } = start(plan);
  await screen.findByText('Контрольный шаг');
  expect(screen.queryByRole('button', { name: 'Подтвердить свою часть' })).not.toBeInTheDocument();
  mock.control.plan.steps[0]!.status = 'not_started'; mock.control.plan.steps[0]!.blocked_reason = null;
  await queryClient.refetchQueries({ queryKey: mentorKeys.plan(id) });
  fireEvent.click(await screen.findByRole('button', { name: 'Подтвердить свою часть' }));
  await waitFor(() => expect(screen.getByRole('progressbar', { name: 'Прогресс плана' })).toHaveAttribute('value', '100'));
});
it('requires explicit repetition after a revision conflict', async () => {
  const { mock } = start(fixture('mentor'), 'checkpoint_conflict'); const spy = vi.spyOn(mentorApi, 'setConfirmation');
  fireEvent.click(await screen.findByRole('button', { name: 'Подтвердить свою часть' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('План изменился'); expect(spy).toHaveBeenCalledTimes(1);
  expect(mock.control.plan.steps[0]?.confirmations).toHaveLength(0);
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить свою часть' }));
  await waitFor(() => expect(mock.control.plan.steps[0]?.status).toBe('completed'));
});
it.each(['checkpoint_lost', 'checkpoint_unconfirmed'] as const)('reconciles %s without inventing success', async scenario => {
  start(fixture('mentor'), scenario);
  fireEvent.click(await screen.findByRole('button', { name: 'Подтвердить свою часть' }));
  if (scenario === 'checkpoint_lost') {
    expect(await screen.findByRole('button', { name: 'Отозвать свою отметку' })).toBeInTheDocument(); expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  } else expect(await screen.findByRole('alert')).toHaveTextContent('Исход действия не подтверждён');
});
it.each([403, 404])('rejects a late PUT after polling denies access with %s, even with a higher revision', async status => {
  const { queryClient } = start(fixture('mentor'));
  let release!: (plan: Plan) => void;
  vi.spyOn(mentorApi, 'setConfirmation').mockImplementation(() => new Promise(resolve => { release = resolve; }));
  fireEvent.click(await screen.findByRole('button', { name: 'Подтвердить свою часть' }));
  await waitFor(() => expect(release).toBeDefined());
  server.use(http.get('*/api/v1/mentor/interns/:id/plan', () => new HttpResponse(null, { status })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.plan(id) });
  await waitFor(() => expect(screen.queryByText('Контрольный шаг')).not.toBeInTheDocument());
  const late = fixture('mentor'); late.steps[0]!.revision = 99;
  await act(async () => release(late));
  expect(queryClient.getQueryData(mentorKeys.plan(id))).toBeNull();
  server.use(http.get('*/api/v1/mentor/interns/:id/plan', () => new HttpResponse(null, { status: 503 })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.plan(id) });
  expect(screen.queryByText('Контрольный шаг')).not.toBeInTheDocument();
});
it('rejects a late response after session cleanup and blocks repeat clicks', async () => {
  const { queryClient } = start(fixture('mentor')); let release!: (plan: Plan) => void;
  const spy = vi.spyOn(mentorApi, 'setConfirmation').mockImplementation(() => new Promise(resolve => { release = resolve; }));
  const button = await screen.findByRole('button', { name: 'Подтвердить свою часть' }); fireEvent.click(button); fireEvent.click(button);
  await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
  await resetSessionState(queryClient); await act(async () => release(fixture('mentor')));
  expect(queryClient.getQueryData(mentorKeys.plan(id))).toBeUndefined();
});
it('polls a completed plan and observes the intern withdrawing their own confirmation', async () => {
  vi.useFakeTimers();
  const { mock } = start(); mock.control.confirmIntern(stepId, true);
  await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Подтвердить свою часть' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить свою часть' }));
  await vi.waitFor(() => expect(screen.getByRole('progressbar', { name: 'Прогресс плана' })).toHaveAttribute('value', '100'));
  mock.control.confirmIntern(stepId, false);
  await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
  await vi.waitFor(() => expect(screen.getByRole('progressbar', { name: 'Прогресс плана' })).toHaveAttribute('value', '0'));
  expect(screen.getByText('Ожидаем подтверждение стажёра')).toBeInTheDocument();
});

it('does not assume a lost confirmation succeeded when its verification returns 503', async () => {
  const { mock } = start(fixture('mentor'), 'checkpoint_lost');
  await screen.findByText('Контрольный шаг');
  server.use(http.get('*/api/v1/mentor/interns/:id/plan', () => new HttpResponse(null, { status: 503 })));
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить свою часть' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Исход действия не подтверждён');
  expect(screen.getByRole('progressbar', { name: 'Прогресс плана' })).toHaveAttribute('value', '0');
  expect(mock.control.plan.steps[0]?.status).toBe('completed');
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить свою часть' }));
  expect(await screen.findByRole('button', { name: 'Отозвать свою отметку' })).toBeInTheDocument();
});
