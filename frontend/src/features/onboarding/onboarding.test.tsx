import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createHandlers } from '@/mocks/handlers';
import { createScenario } from '@/mocks/fixtures/scenario';
import { planSchema } from '@/features/onboarding/schemas';
import { accessRequestSchema } from '@/features/access-requests/schemas';
import { routes } from '@/app/router';

function renderPlan() {
  const memoryRouter = createMemoryRouter(routes, { initialEntries: ['/onboarding'] });
  renderWithQuery(<RouterProvider router={memoryRouter} />);
  return memoryRouter;
}

describe('onboarding plan (#4)', () => {
  it('renders server steps, progress and the next step highlight', async () => {
    renderPlan();
    expect(await screen.findByText('Подготовка к первой задаче')).toBeInTheDocument();
    expect(screen.getByText('Запустить проект')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0');
    expect(screen.getByText(/Следующий шаг: «Запустить проект»/)).toBeInTheDocument();
    expect(screen.getByText('Прогресс: 0%')).toBeInTheDocument();
  });

  it('switches one visible step with arrows and status dots', async () => {
    const user = userEvent.setup();
    renderPlan();

    expect(await screen.findByRole('heading', { name: 'Запустить проект' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Предыдущий шаг' })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^Шаг 1:/ })).toHaveAttribute('aria-current', 'step');

    await user.click(screen.getByRole('button', { name: 'Следующий шаг' }));
    expect(screen.getByRole('heading', { name: 'Организационные моменты' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Запустить проект' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Шаг 2:/ })).toHaveAttribute('aria-current', 'step');
    expect(screen.getByRole('button', { name: 'Предыдущий шаг' })).toBeEnabled();

    await user.click(screen.getByRole('button', { name: /^Шаг 1:/ }));
    expect(screen.getByRole('heading', { name: 'Запустить проект' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Предыдущий шаг' })).toBeDisabled();

    await user.click(screen.getByRole('button', { name: 'Следующий шаг' }));
    expect(screen.getByRole('heading', { name: 'Организационные моменты' })).toBeInTheDocument();
  });

  it('links "Спросить помощника" to /chat with the stepId query param', async () => {
    renderPlan();
    const ask = await screen.findByRole('link', { name: 'Спросить помощника о шаге: Запустить проект' });
    expect(ask).toHaveAttribute('href', '/chat?stepId=step_local_setup');
  });

  it('shows related access requests inside the step card', async () => {
    const f = createScenario();
    renderPlan();
    expect(await screen.findByText('Тестовый репозиторий')).toBeInTheDocument();
    expect(screen.getByText('Черновик')).toBeInTheDocument();
    expect(screen.getByText(f.draft.reason)).toBeInTheDocument();
  });

  it('applies the returned plan after a successful PATCH and blocks repeats while pending', async () => {
    const user = userEvent.setup();
    let releasePatch!: (value: unknown) => void;
    const gate = new Promise((resolve) => { releasePatch = resolve; });
    let patchCount = 0;
    server.resetHandlers(...[
      http.patch('*/api/v1/onboarding/steps/:stepId', async () => {
        patchCount += 1;
        await gate;
        return HttpResponse.json({
          ...createScenario().plan,
          steps: [{
            ...createScenario().plan.steps[0]!,
            status: 'not_started',
            allowed_statuses: ['in_progress'],
          }],
        });
      }),
    ], ...createHandlers());
    renderPlan();
    const button = await screen.findByRole('button', { name: /сбросить в не начатые/i });

    await user.click(button);
    // Repeat clicks are blocked for the duration of the request
    expect(button).toBeDisabled();
    await waitFor(() => expect(patchCount).toBe(1));
    releasePatch(null);

    expect(await screen.findByText('Не начат')).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: /начать шаг/i })).toBeInTheDocument();
  });

  it('turns a 409 into a visible reason without applying a local success', async () => {
    const user = userEvent.setup();
    let planGets = 0;
    server.resetHandlers(...[
      http.get('*/api/v1/onboarding/plan', () => {
        planGets += 1;
        return HttpResponse.json(createScenario().plan);
      }),
      http.patch('*/api/v1/onboarding/steps/:stepId', () => HttpResponse.json(
        { error: { code: 'STEP_BLOCKED', message: 'Ожидается доступ к репозиторию', trace_id: 'trace_mock' } },
        { status: 409 },
      )),
    ], ...createHandlers());
    renderPlan();
    await user.click(await screen.findByRole('button', { name: /сбросить в не начатые/i }));

    // The server reason is shown; the step status is not flipped locally
    expect(await screen.findByRole('alert')).toHaveTextContent('Ожидается доступ к репозиторию');
    expect(screen.getByText('В работе')).toBeInTheDocument();
    // The plan is refreshed from the server after the conflict
    await waitFor(() => expect(planGets).toBeGreaterThanOrEqual(2));
  });

  it('renders the completed state when the server reports a finished plan', async () => {
    server.resetHandlers(...[
      http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(createScenario().completed_plan)),
    ], ...createHandlers());
    renderPlan();
    expect(await screen.findByText('Выполнен')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100');
    expect(screen.getByText(/План завершён/)).toBeInTheDocument();
  });

  it('explains blocked steps with the server blocked_reason and no action buttons', async () => {
    server.resetHandlers(...[
      http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(createScenario().blocked_plan)),
    ], ...createHandlers());
    renderPlan();
    expect(await screen.findByText('Заблокирован')).toBeInTheDocument();
    expect(screen.getByText('Ожидается доступ к репозиторию')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /начать шаг|выполненным/i })).not.toBeInTheDocument();
    expect(screen.getByText(/Оставшиеся шаги заблокированы/)).toBeInTheDocument();
  });

  it('renders an empty state for a plan without steps', async () => {
    server.resetHandlers(...[
      http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(
        planSchema.parse({ id: 'plan_empty', title: 'Пустой план', progress_percent: 0, next_step_id: null, steps: [] }),
      )),
    ], ...createHandlers());
    renderPlan();
    expect(await screen.findByText('Шагов пока нет')).toBeInTheDocument();
  });

  it('surfaces a load error with retry instead of an empty plan', async () => {
    server.resetHandlers(...[
      http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(
        { error: { code: 'INTERNAL', message: 'План временно недоступен', trace_id: 'trace_mock' } },
        { status: 503 },
      )),
    ], ...createHandlers());
    renderPlan();
    expect(await screen.findByRole('alert')).toHaveTextContent('План временно недоступен');
  });

  it('keeps request panels consistent with the runtime schema', async () => {
    const f = createScenario();
    const parsed = accessRequestSchema.parse(f.draft);
    expect(parsed.resource.permission).toBe('read');
    renderPlan();
    expect(await screen.findByText('Право: чтение')).toBeInTheDocument();
  });
});
