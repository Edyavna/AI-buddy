import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import { focusManager } from '@tanstack/react-query';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createHandlers } from '@/mocks/handlers';
import { createScenario } from '@/mocks/fixtures/scenario';
import { planSchema, stepSchema } from '@/features/onboarding/schemas';
import type { Plan, Step } from '@/features/onboarding/schemas';
import { queryKeys } from '@/shared/api';
import { routes } from '@/app/router';

const confirmedAt = '2026-11-12T12:00:00Z';
const internConfirmation = { role: 'intern' as const, user_id: 'intern_demo_1', confirmed_at: confirmedAt };
const mentorConfirmation = { role: 'mentor' as const, user_id: 'mentor_demo_1', confirmed_at: confirmedAt };

function checkpointStep(overrides: Partial<Step> = {}): Step {
  return stepSchema.parse({
    id: 'step_checkpoint',
    title: 'Обсудить цели',
    description: 'Встреча с наставником',
    status: 'not_started',
    request_ids: [],
    allowed_statuses: ['in_progress', 'completed'],
    blocked_reason: null,
    template_key: 'discuss_goals',
    period: '2 неделя',
    completion_policy: 'intern',
    confirmations: [],
    revision: 1,
    allowed_actions: ['confirm'],
    awaiting_role: null,
    instructions: '1. Согласуйте цели\n2. Зафиксируйте критерии успеха',
    resources: [],
    ...overrides,
  });
}

function checkpointPlan(step: Step): Plan {
  return planSchema.parse({
    id: 'plan_checkpoint',
    title: 'План стажировки',
    progress_percent: step.status === 'completed' ? 100 : 0,
    next_step_id: step.status === 'completed' || step.status === 'blocked' ? null : step.id,
    steps: [step],
  });
}

function renderPlan(cachedPlan?: Plan) {
  const router = createMemoryRouter(routes, { initialEntries: ['/onboarding'] });
  return {
    router,
    ...renderWithQuery(<RouterProvider router={router} />, (queryClient) => {
      if (cachedPlan) queryClient.setQueryData(queryKeys.plan(), cachedPlan);
    }),
  };
}

function usePlanHandler(getPlan: () => Plan, onGet?: () => void) {
  return http.get('*/api/v1/onboarding/plan', () => {
    onGet?.();
    return HttpResponse.json(getPlan());
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe('checkpoint catalog and policies (#46)', () => {
  it('renders every catalog step through the switcher with unique keys and full instructions', async () => {
    const user = userEvent.setup();
    renderPlan();

    await screen.findByRole('heading', { name: 'Запустить проект' });
    const dots = screen.getAllByRole('button', { name: /^Шаг \d+:/ });
    expect(dots).toHaveLength(17);

    const stepIds: string[] = [];
    for (const dot of dots) {
      await user.click(dot);
      const card = document.querySelector<HTMLElement>('article[data-step-id]');
      expect(card).toBeInTheDocument();
      stepIds.push(card!.dataset.stepId!);
    }
    expect(new Set(stepIds).size).toBe(17);

    await user.click(dots[1]!);
    const organization = await screen.findByRole('heading', { name: 'Организационные моменты' });
    expect(organization.closest('article')).toHaveAttribute('data-step-id', 'step_organization');
    expect(within(organization.closest('article')!).getByText(/Добавь стажёра во все необходимые чаты/)).toBeInTheDocument();
    expect(within(organization.closest('article')!).getByText(/Обсуди цели и план на период адаптации/)).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /: Экваторная встреча,/ })).toHaveLength(2);
    expect(screen.getAllByRole('button', { name: /: Пройди опрос о том, как прошла стажировка,/ }).length).toBeGreaterThanOrEqual(2);
    expect(within(organization.closest('article')!).getByRole('link', { name: /Спросить помощника/ }))
      .toHaveAttribute('href', '/chat?stepId=step_organization');

    await user.click(screen.getByRole('button', { name: /^Шаг \d+: Изучи все необходимые документы/ }));
    expect(screen.getByRole('heading', { name: /Изучи все необходимые документы и мануалы по работе в своей команде/ })).toBeInTheDocument();
  });

  it('keeps legacy plans compatible and applies checkpoint defaults at the runtime boundary', () => {
    const legacy = planSchema.parse(createScenario().plan);
    expect(legacy.steps[0]).toMatchObject({
      template_key: 'step_local_setup',
      period: null,
      completion_policy: 'intern',
      confirmations: [],
      revision: 1,
      allowed_actions: [],
      instructions: 'Тестовый шаг настройки окружения',
      resources: [],
    });
  });

  it('renders seeded instructions as a list when description contains the same text', async () => {
    const instructions = '1. Согласуйте цели\n 2. Зафиксируйте критерии успеха';
    const step = checkpointStep({ description: instructions, instructions });
    server.use(usePlanHandler(() => checkpointPlan(step)));
    renderPlan();

    const heading = await screen.findByRole('heading', { name: step.title });
    const instructionsSection = within(heading.closest('article')!).getByRole('region', { name: `Инструкция: ${step.title}` });
    expect(within(instructionsSection).getAllByRole('listitem').map((item) => item.textContent)).toEqual([
      'Согласуйте цели',
      'Зафиксируйте критерии успеха',
    ]);
  });

  it('normalizes a legacy cached plan before rendering it', async () => {
    renderPlan(createScenario().plan as unknown as Plan);

    expect(await screen.findByRole('heading', { name: 'Запустить проект' })).toBeInTheDocument();
    expect(screen.queryByText(/Cannot read properties of undefined/)).not.toBeInTheDocument();
  });

  it('shows only safe material links and explains resources without a URL', async () => {
    const step = checkpointStep({
      resources: [
        { title: 'Безопасная инструкция', url: 'https://example.com/guide', note: null },
        { title: 'Небезопасная ссылка', url: 'javascript:alert(1)', note: null },
        { title: 'Материал без адреса', url: null, note: null },
      ],
    });
    server.resetHandlers(usePlanHandler(() => checkpointPlan(step)), ...createHandlers());
    renderPlan();

    expect(await screen.findByRole('link', { name: 'Безопасная инструкция' }))
      .toHaveAttribute('href', 'https://example.com/guide');
    expect(screen.queryByRole('link', { name: 'Небезопасная ссылка' })).not.toBeInTheDocument();
    expect(screen.getByText('Небезопасная ссылка')).toBeInTheDocument();
    expect(screen.getByText('Материал без адреса')).toBeInTheDocument();
    expect(screen.getAllByText('Уточнить у наставника')).toHaveLength(2);
  });

  it('confirms and revokes an intern checkpoint from the keyboard using server actions', async () => {
    const user = userEvent.setup();
    const initial = checkpointPlan(checkpointStep());
    const completed = checkpointPlan(checkpointStep({
      status: 'completed',
      confirmations: [internConfirmation],
      revision: 2,
      allowed_actions: ['revoke'],
      allowed_statuses: ['in_progress'],
    }));
    const revoked = checkpointPlan(checkpointStep({ status: 'in_progress', revision: 3 }));
    let current = initial;
    const bodies: unknown[] = [];
    server.resetHandlers(
      usePlanHandler(() => current),
      http.put('*/api/v1/onboarding/steps/:stepId/confirmation', async ({ request }) => {
        bodies.push(await request.json());
        current = bodies.length === 1 ? completed : revoked;
        return HttpResponse.json(current);
      }),
      ...createHandlers(),
    );
    renderPlan();

    const confirm = await screen.findByRole('button', { name: 'Подтвердить выполнение' });
    expect(screen.queryByRole('button', { name: 'Отметить выполненным' })).not.toBeInTheDocument();
    confirm.focus();
    await user.keyboard('{Enter}');

    expect(await screen.findByText('Выполнен')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100');
    expect(screen.getByText('Вы подтвердили')).toBeInTheDocument();
    expect(bodies[0]).toEqual({ confirmed: true, revision: 1 });

    await user.click(screen.getByRole('button', { name: 'Отозвать подтверждение' }));
    expect(await screen.findByRole('button', { name: 'Подтвердить выполнение' })).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0');
    expect(bodies[1]).toEqual({ confirmed: false, revision: 2 });
  });

  it('shows mentor policy without an intern completion action', async () => {
    const step = checkpointStep({
      completion_policy: 'mentor',
      allowed_actions: [],
      allowed_statuses: [],
    });
    server.resetHandlers(usePlanHandler(() => checkpointPlan(step)), ...createHandlers());
    renderPlan();

    expect(await screen.findByText('Подтверждает наставник')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Подтвердить выполнение' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Отметить выполненным' })).not.toBeInTheDocument();
  });

  it('keeps both incomplete after intern first and picks up mentor confirmation on refetch', async () => {
    const initial = checkpointPlan(checkpointStep({
      completion_policy: 'both',
      allowed_statuses: ['in_progress'],
    }));
    const internFirst = checkpointPlan(checkpointStep({
      completion_policy: 'both',
      status: 'in_progress',
      confirmations: [internConfirmation],
      revision: 2,
      allowed_actions: ['revoke'],
      allowed_statuses: [],
      awaiting_role: 'mentor',
    }));
    const completed = checkpointPlan(checkpointStep({
      completion_policy: 'both',
      status: 'completed',
      confirmations: [internConfirmation, mentorConfirmation],
      revision: 3,
      allowed_actions: ['revoke'],
      allowed_statuses: [],
    }));
    let current = initial;
    server.resetHandlers(
      usePlanHandler(() => current),
      http.put('*/api/v1/onboarding/steps/:stepId/confirmation', () => {
        current = internFirst;
        return HttpResponse.json(current);
      }),
      ...createHandlers(),
    );
    const view = renderPlan();

    await userEvent.click(await screen.findByRole('button', { name: 'Подтвердить выполнение' }));
    expect(await screen.findByText('Ожидаем наставника')).toBeInTheDocument();
    expect(screen.queryByText('Выполнен')).not.toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0');
    expect(screen.getByText('Подтверждено')).toBeInTheDocument();
    expect(screen.getByText('Ожидается')).toBeInTheDocument();

    current = completed;
    await view.queryClient.refetchQueries({ queryKey: queryKeys.plan() });
    expect(await screen.findByText('Выполнен')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100');
  });

  it('completes both when mentor confirmed first and preserves that confirmation after intern revoke', async () => {
    const mentorFirst = checkpointPlan(checkpointStep({
      completion_policy: 'both',
      status: 'in_progress',
      confirmations: [mentorConfirmation],
      revision: 2,
      allowed_actions: ['confirm'],
      allowed_statuses: [],
      awaiting_role: 'intern',
    }));
    const completed = checkpointPlan(checkpointStep({
      completion_policy: 'both',
      status: 'completed',
      confirmations: [mentorConfirmation, internConfirmation],
      revision: 3,
      allowed_actions: ['revoke'],
      allowed_statuses: [],
    }));
    const revoked = checkpointPlan(checkpointStep({
      completion_policy: 'both',
      status: 'in_progress',
      confirmations: [mentorConfirmation],
      revision: 4,
      allowed_actions: ['confirm'],
      allowed_statuses: [],
      awaiting_role: 'intern',
    }));
    let current = mentorFirst;
    let revoke = false;
    server.resetHandlers(
      usePlanHandler(() => current),
      http.put('*/api/v1/onboarding/steps/:stepId/confirmation', () => {
        if (!revoke) {
          current = completed;
          revoke = true;
        } else {
          current = revoked;
        }
        return HttpResponse.json(current);
      }),
      ...createHandlers(),
    );
    renderPlan();

    expect(await screen.findByText('Наставник уже подтвердил. Осталось ваше подтверждение.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Подтвердить выполнение' }));
    expect(await screen.findByText('Выполнен')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Отозвать подтверждение' }));
    expect(await screen.findByText('Наставник уже подтвердил. Осталось ваше подтверждение.')).toBeInTheDocument();
    expect(screen.queryByText('Выполнен')).not.toBeInTheDocument();
    expect(screen.getByText('Подтверждено')).toBeInTheDocument();
  });

  it('does not invent actions for a blocked checkpoint', async () => {
    const blocked = checkpointStep({
      completion_policy: 'both',
      status: 'blocked',
      blocked_reason: 'Сначала завершите предыдущий шаг',
      allowed_actions: [],
      allowed_statuses: [],
    });
    server.resetHandlers(usePlanHandler(() => checkpointPlan(blocked)), ...createHandlers());
    renderPlan();

    expect(await screen.findByText('Сначала завершите предыдущий шаг')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Подтвердить выполнение' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Отозвать подтверждение' })).not.toBeInTheDocument();
  });
});

describe('checkpoint synchronization (#46)', () => {
  it('refreshes after a revision conflict and requires an explicit new action', async () => {
    const initial = checkpointPlan(checkpointStep());
    const changed = checkpointPlan(checkpointStep({
      status: 'in_progress',
      confirmations: [mentorConfirmation],
      completion_policy: 'both',
      revision: 2,
      awaiting_role: 'intern',
    }));
    let current = initial;
    let gets = 0;
    server.resetHandlers(
      usePlanHandler(() => current, () => { gets += 1; }),
      http.put('*/api/v1/onboarding/steps/:stepId/confirmation', () => {
        current = changed;
        return HttpResponse.json({
          error: {
            code: 'CHECKPOINT_REVISION_CONFLICT',
            message: 'Обновите план',
            trace_id: 'trace_conflict',
          },
        }, { status: 409 });
      }),
      ...createHandlers(),
    );
    renderPlan();

    await userEvent.click(await screen.findByRole('button', { name: 'Подтвердить выполнение' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('План изменился. Проверьте актуальное состояние и повторите действие.');
    expect(screen.getByText('Наставник уже подтвердил. Осталось ваше подтверждение.')).toBeInTheDocument();
    expect(screen.queryByText('Выполнен')).not.toBeInTheDocument();
    expect(gets).toBeGreaterThanOrEqual(2);
    expect(screen.getByRole('button', { name: 'Подтвердить выполнение' })).toBeEnabled();
  });

  it('reconciles a lost PUT response and accepts the persisted confirmation', async () => {
    const initial = checkpointPlan(checkpointStep());
    const completed = checkpointPlan(checkpointStep({
      status: 'completed',
      confirmations: [internConfirmation],
      revision: 2,
      allowed_actions: ['revoke'],
      allowed_statuses: ['in_progress'],
    }));
    let current = initial;
    let gets = 0;
    server.resetHandlers(
      usePlanHandler(() => current, () => { gets += 1; }),
      http.put('*/api/v1/onboarding/steps/:stepId/confirmation', () => {
        current = completed;
        return HttpResponse.error();
      }),
      ...createHandlers(),
    );
    renderPlan();

    await userEvent.click(await screen.findByRole('button', { name: 'Подтвердить выполнение' }));
    expect(await screen.findByText('Выполнен')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(gets).toBeGreaterThanOrEqual(2);
  });

  it('reports an unconfirmed outcome without inventing success when reconciliation finds no change', async () => {
    const initial = checkpointPlan(checkpointStep());
    server.resetHandlers(
      usePlanHandler(() => initial),
      http.put('*/api/v1/onboarding/steps/:stepId/confirmation', () => HttpResponse.error()),
      ...createHandlers(),
    );
    renderPlan();

    await userEvent.click(await screen.findByRole('button', { name: 'Подтвердить выполнение' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Исход действия не подтверждён');
    expect(screen.queryByText('Выполнен')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Подтвердить выполнение' })).toBeEnabled();
  });

  it('polls an active plan and refetches when the window regains focus', async () => {
    vi.useFakeTimers();
    const initial = checkpointPlan(checkpointStep());
    let gets = 0;
    server.resetHandlers(usePlanHandler(() => initial, () => { gets += 1; }), ...createHandlers());
    renderPlan();

    await vi.waitFor(() => expect(gets).toBe(1));
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    await vi.waitFor(() => expect(gets).toBeGreaterThanOrEqual(2));
    const beforeFocus = gets;
    await act(async () => {
      focusManager.setFocused(false);
      focusManager.setFocused(true);
    });
    await vi.waitFor(() => expect(gets).toBeGreaterThan(beforeFocus));
  });

  it('does not bring back a revoked plan when a later refresh returns 503', async () => {
    const initial = checkpointPlan(checkpointStep({ title: 'Закрытый план' }));
    let response = 200;
    server.resetHandlers(http.get('*/api/v1/onboarding/plan', () => {
      if (response === 200) return HttpResponse.json(initial);
      return HttpResponse.json({
        error: { code: response === 403 ? 'ROLE_FORBIDDEN' : 'SERVICE_UNAVAILABLE', message: 'План недоступен', trace_id: 'trace_plan' },
      }, { status: response });
    }), ...createHandlers());
    const view = renderPlan();

    expect(await screen.findByText('Закрытый план')).toBeInTheDocument();
    response = 403;
    await view.queryClient.refetchQueries({ queryKey: queryKeys.plan() });
    await waitFor(() => expect(screen.queryByText('Закрытый план')).not.toBeInTheDocument());
    expect(view.queryClient.getQueryData(queryKeys.plan())).toBeNull();

    response = 503;
    await view.queryClient.refetchQueries({ queryKey: queryKeys.plan() });
    expect(screen.queryByText('Закрытый план')).not.toBeInTheDocument();
    expect(view.queryClient.getQueryData(queryKeys.plan())).toBeNull();
  });
});

it('keeps polling a completed plan and observes a mentor revocation without refocusing', async () => {
  vi.useFakeTimers();
  let current = checkpointPlan(checkpointStep({
    completion_policy: 'both', status: 'completed', revision: 3,
    confirmations: [internConfirmation, mentorConfirmation], allowed_actions: ['revoke'],
  }));
  let gets = 0;
  server.resetHandlers(usePlanHandler(() => current, () => { gets += 1; }), ...createHandlers());
  renderPlan();
  await vi.waitFor(() => expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100'));
  current = checkpointPlan(checkpointStep({
    completion_policy: 'both', status: 'in_progress', revision: 4,
    confirmations: [internConfirmation], allowed_actions: ['revoke'], awaiting_role: 'mentor',
  }));
  await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
  await vi.waitFor(() => expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0'));
  expect(screen.getByText('Ожидаем наставника')).toBeInTheDocument();
  expect(gets).toBeGreaterThanOrEqual(2);
});
