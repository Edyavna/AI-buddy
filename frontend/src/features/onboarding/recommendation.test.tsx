import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createFullCheckpointPlan } from '@/mocks/fixtures/checkpoints';
import { routes } from '@/app/router';
import { onboardingApi } from './api';
import { RecommendationCard, formatWaitingDuration } from './RecommendationCard';
import type { Recommendation } from './schemas';

describe('formatWaitingDuration', () => {
  it('returns null for null, undefined or negative numbers', () => {
    expect(formatWaitingDuration(null)).toBeNull();
    expect(formatWaitingDuration(undefined)).toBeNull();
    expect(formatWaitingDuration(-10)).toBeNull();
  });

  it('formats less than a minute', () => {
    expect(formatWaitingDuration(0)).toBe('меньше минуты');
    expect(formatWaitingDuration(45)).toBe('меньше минуты');
    expect(formatWaitingDuration(59)).toBe('меньше минуты');
  });

  it('formats minutes', () => {
    expect(formatWaitingDuration(60)).toBe('1 мин');
    expect(formatWaitingDuration(120)).toBe('2 мин');
    expect(formatWaitingDuration(59 * 60 + 30)).toBe('59 мин');
  });

  it('formats hours with remaining minutes', () => {
    expect(formatWaitingDuration(3600)).toBe('1 ч');
    expect(formatWaitingDuration(3600 * 2)).toBe('2 ч');
    expect(formatWaitingDuration(3600 + 15 * 60)).toBe('1 ч 15 мин');
    expect(formatWaitingDuration(3600 * 5 + 40 * 60)).toBe('5 ч 40 мин');
  });

  it('formats days with remaining hours', () => {
    expect(formatWaitingDuration(86400)).toBe('1 дн');
    expect(formatWaitingDuration(86400 * 3)).toBe('3 дн');
    expect(formatWaitingDuration(86400 * 2 + 3600 * 4)).toBe('2 дн 4 ч');
  });
});

describe('RecommendationCard component', () => {
  const baseAvailable: Recommendation = {
    state: 'action_available',
    step_id: 'step_local_setup',
    title: 'Настройка окружения',
    period: '1 день',
    reason: 'Текущий доступный шаг по плану адаптации',
    action_type: 'confirm',
    action_label: 'Подтвердить выполнение',
    target: {
      step_id: 'step_local_setup',
      route: '/onboarding?stepId=step_local_setup',
      action: 'open_card',
    },
  };

  const renderWithRouter = (ui: React.ReactElement, initialPath = '/onboarding') => {
    const router = createMemoryRouter([{ path: '*', element: ui }], {
      initialEntries: [initialPath],
    });
    return {
      router,
      ...render(<RouterProvider router={router} />),
    };
  };

  it('renders available action with badge, period, reason, and action button', async () => {
    const onSelectStep = vi.fn();
    renderWithRouter(
      <RecommendationCard
        recommendation={baseAvailable}
        onSelectStep={onSelectStep}
      />
    );

    expect(screen.getByTestId('recommendation-card')).toHaveAttribute('data-state', 'action_available');
    expect(screen.getByText('Рекомендуемый шаг')).toBeInTheDocument();
    expect(screen.getByTestId('recommendation-period')).toHaveTextContent('1 день');
    expect(screen.getByTestId('recommendation-title')).toHaveTextContent('Шаг: «Настройка окружения»');
    expect(screen.getByTestId('recommendation-reason')).toHaveTextContent(
      'Текущий доступный шаг по плану адаптации'
    );

    const actionBtn = screen.getByTestId('recommendation-action-btn');
    expect(actionBtn).toHaveTextContent('Подтвердить выполнение');
    await userEvent.click(actionBtn);
    expect(onSelectStep).toHaveBeenCalledWith('step_local_setup');
  });

  it('navigates to chat when target action is open_chat', async () => {
    const chatRecommendation: Recommendation = {
      ...baseAvailable,
      action_type: 'assistant',
      action_label: 'Спросить в чате',
      target: {
        step_id: 'step_local_setup',
        route: '/chat?stepId=step_local_setup',
        action: 'open_chat',
      },
    };

    const { router } = renderWithRouter(
      <RecommendationCard recommendation={chatRecommendation} />
    );

    await userEvent.click(screen.getByTestId('recommendation-action-btn'));
    expect(router.state.location.pathname).toBe('/chat');
    expect(router.state.location.search).toBe('?stepId=step_local_setup');
  });

  it('renders priority waiting notice with formatted duration and step switch link', async () => {
    const onSelectStep = vi.fn();
    const withWaitingStep: Recommendation = {
      ...baseAvailable,
      reason: 'Приоритетный шаг ожидает проверки, доступен следующий независимый шаг',
      waiting_step: {
        step_id: 'step_priority',
        title: 'Первый PR в репозиторий',
        period: '1 неделя',
        reason: 'Ожидается подтверждение наставника',
        state: 'waiting_mentor',
        awaiting_role: 'mentor',
        help_direction: 'mentor',
        blocked_at: '2026-11-14T10:00:00Z',
        waiting_duration_seconds: 7200,
      },
    };

    renderWithRouter(
      <RecommendationCard
        recommendation={withWaitingStep}
        onSelectStep={onSelectStep}
      />
    );

    const notice = screen.getByTestId('waiting-step-notice');
    expect(notice).toHaveTextContent('«Первый PR в репозиторий»');
    expect(notice).toHaveTextContent('(1 неделя)');
    expect(notice).toHaveTextContent('Ожидается подтверждение наставника');
    expect(screen.getByTestId('waiting-duration')).toHaveTextContent('В ожидании: 2 ч');

    const viewBtn = screen.getByRole('button', {
      name: 'Перейти к шагу Первый PR в репозиторий',
    });
    await userEvent.click(viewBtn);
    expect(onSelectStep).toHaveBeenCalledWith('step_priority');
  });

  it('renders waiting_mentor state with help action link to mentor', () => {
    const rec: Recommendation = {
      state: 'waiting_mentor',
      title: 'Встреча 1-на-1',
      period: '1 неделя',
      reason: 'Шаг ожидает подтверждения наставником',
      help_action: {
        direction: 'mentor',
        label: 'Написать наставнику в чат',
        url: '/chat?topic=mentor-review',
        available: true,
      },
      waiting_step: {
        step_id: 'step_meeting',
        title: 'Встреча 1-на-1',
        reason: 'Проверка выполнения наставником',
        state: 'waiting_mentor',
        waiting_duration_seconds: 3720,
      },
    };

    renderWithRouter(<RecommendationCard recommendation={rec} />);

    expect(screen.getByText('Ожидает наставника')).toBeInTheDocument();
    expect(screen.getByTestId('waiting-duration')).toHaveTextContent('В ожидании: 1 ч 2 мин');
    const helpLink = screen.getByTestId('help-action-btn');
    expect(helpLink).toHaveTextContent('Написать наставнику в чат');
    expect(helpLink).toHaveAttribute('href', '/chat?topic=mentor-review');
  });

  it('renders waiting_access state and external help link', () => {
    const rec: Recommendation = {
      state: 'waiting_access',
      title: 'Доступ в production',
      period: '2 неделя',
      reason: 'Ожидается согласование заявки на доступ к репозиторию',
      help_action: {
        direction: 'mentor',
        label: 'Портал доступов',
        url: 'https://idm.avito.ru/requests',
        available: true,
      },
    };

    renderWithRouter(<RecommendationCard recommendation={rec} />);

    expect(screen.getByText('Ожидает доступов')).toBeInTheDocument();
    const helpLink = screen.getByTestId('help-action-btn');
    expect(helpLink).toHaveAttribute('href', 'https://idm.avito.ru/requests');
    expect(helpLink).toHaveAttribute('target', '_blank');
  });

  it('renders blocked_dependencies state', () => {
    const rec: Recommendation = {
      state: 'blocked_dependencies',
      title: 'Деплой в staging',
      period: '3 неделя',
      reason: 'Шаг зависит от незавершённых предыдущих шагов',
    };

    renderWithRouter(<RecommendationCard recommendation={rec} />);

    expect(screen.getByText('Заблокировано зависимостями')).toBeInTheDocument();
    expect(screen.getByTestId('recommendation-reason')).toHaveTextContent(
      'Шаг зависит от незавершённых предыдущих шагов'
    );
  });

  it('renders general blocked state', () => {
    const rec: Recommendation = {
      state: 'blocked',
      title: 'Сложный шаг',
      reason: 'Все оставшиеся шаги заблокированы',
    };

    renderWithRouter(<RecommendationCard recommendation={rec} />);

    expect(screen.getByText('Заблокирован')).toBeInTheDocument();
  });

  it('renders fallback notice when help action is unavailable (no HR in team)', () => {
    const rec: Recommendation = {
      state: 'waiting_mentor',
      title: 'Оформление пропуска',
      period: '1 день',
      reason: 'Шаг оформляется через HR',
      help_action: {
        direction: 'hr',
        label: 'Связаться с HR',
        url: '/chat?topic=hr-support',
        available: false,
      },
    };

    renderWithRouter(<RecommendationCard recommendation={rec} />);

    expect(screen.getByTestId('no-help-notice')).toBeInTheDocument();
    expect(screen.getByText(/В вашей команде пока нет активного HR-специалиста/)).toBeInTheDocument();
  });

  it('renders empty_plan state with appropriate message', () => {
    const rec: Recommendation = {
      state: 'empty_plan',
      reason: 'План адаптации пуст. Наставник скоро добавит первые шаги.',
    };

    renderWithRouter(<RecommendationCard recommendation={rec} />);

    expect(screen.getByText('План пуст')).toBeInTheDocument();
    expect(screen.getByText('План адаптации формируется')).toBeInTheDocument();
    expect(
      screen.getByText('План адаптации пуст. Наставник скоро добавит первые шаги.')
    ).toBeInTheDocument();
  });

  it('renders completed state with congratulations and completion reason', () => {
    const rec: Recommendation = {
      state: 'completed',
      reason: 'Все шаги плана успешно завершены!',
    };

    renderWithRouter(<RecommendationCard recommendation={rec} />);

    expect(screen.getByText('Все задачи выполнены')).toBeInTheDocument();
    expect(screen.getByText('Все задачи выполнены 🎉')).toBeInTheDocument();
    expect(screen.getByText('Все шаги плана успешно завершены!')).toBeInTheDocument();
  });
});

describe('PlanPage recommendation integration & URL persistence (#55)', () => {
  const renderPlanApp = (initialEntry = '/onboarding') => {
    const router = createMemoryRouter(routes, { initialEntries: [initialEntry] });
    return {
      router,
      ...renderWithQuery(<RouterProvider router={router} />),
    };
  };

  it('displays recommendation card on /onboarding and selects step via recommendation button', async () => {
    const { router } = renderPlanApp('/onboarding');

    // Recommendation card is displayed on the intern home page
    const card = await screen.findByTestId('recommendation-card');
    expect(card).toBeInTheDocument();
    expect(screen.getByTestId('recommendation-title')).toBeInTheDocument();

    // Clicking the recommendation action button selects the step
    const actionBtn = screen.getByTestId('recommendation-action-btn');
    await userEvent.click(actionBtn);

    // The URL search params are updated with ?stepId=...
    expect(router.state.location.search).toContain('stepId=');
  });

  it('preserves selected step from URL search param on page load / reload', async () => {
    // Visit page directly with ?stepId=step_organization
    const { router } = renderPlanApp('/onboarding?stepId=step_organization');

    // The step corresponding to step_organization is loaded and displayed
    expect(await screen.findByRole('heading', { name: 'Организационные моменты' })).toBeInTheDocument();
    expect(router.state.location.search).toBe('?stepId=step_organization');
  });

  it('updates URL search params when user navigates through carousel steps', async () => {
    const { router } = renderPlanApp('/onboarding');

    // Wait for plan to load
    expect(await screen.findByRole('heading', { name: 'Запустить проект' })).toBeInTheDocument();

    // Click next step button
    const nextBtn = screen.getByRole('button', { name: 'Следующий шаг' });
    await userEvent.click(nextBtn);

    // Heading changes and search params reflect the new step
    expect(await screen.findByRole('heading', { name: 'Организационные моменты' })).toBeInTheDocument();
    expect(router.state.location.search).toBe('?stepId=step_organization');
  });

  it('selects step card when recommendation target has action open_card', async () => {
    const fullPlan = createFullCheckpointPlan();
    const customPlan = {
      ...fullPlan,
      recommendation: {
        state: 'action_available' as const,
        step_id: 'step_organization',
        title: 'Организационные моменты',
        period: '1 день',
        reason: 'Следующий независимый шаг',
        action_type: 'confirm' as const,
        action_label: 'Открыть карточку',
        target: {
          step_id: 'step_organization',
          route: '/onboarding?stepId=step_organization',
          action: 'open_card' as const,
        },
      },
    };
    server.use(
      http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(customPlan))
    );

    const { router } = renderPlanApp('/onboarding');
    expect(await screen.findByRole('heading', { name: 'Запустить проект' })).toBeInTheDocument();

    const actionBtn = screen.getByTestId('recommendation-action-btn');
    expect(actionBtn).toHaveTextContent('Открыть карточку');
    await userEvent.click(actionBtn);

    expect(await screen.findByRole('heading', { name: 'Организационные моменты' })).toBeInTheDocument();
    expect(router.state.location.search).toBe('?stepId=step_organization');
  });

  it('selects waiting step when clicking view waiting step button', async () => {
    const fullPlan = createFullCheckpointPlan();
    const withWaiting = {
      ...fullPlan,
      recommendation: {
        state: 'action_available' as const,
        step_id: 'step_organization',
        title: 'Организационные моменты',
        period: '1 день',
        reason: 'Текущий шаг',
        action_type: 'confirm' as const,
        action_label: 'Подтвердить',
        target: {
          step_id: 'step_organization',
          route: '/onboarding',
          action: 'open_card' as const,
        },
        waiting_step: {
          step_id: 'step_local_setup',
          title: 'Запустить проект',
          reason: 'Ожидает наставника',
          state: 'waiting_mentor' as const,
        },
      },
    };
    server.use(
      http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(withWaiting))
    );

    const { router } = renderPlanApp('/onboarding?stepId=step_organization');
    expect(await screen.findByRole('heading', { name: 'Организационные моменты' })).toBeInTheDocument();

    const viewWaitingBtn = screen.getByRole('button', { name: 'Перейти к шагу Запустить проект' });
    await userEvent.click(viewWaitingBtn);

    expect(await screen.findByRole('heading', { name: 'Запустить проект' })).toBeInTheDocument();
    expect(router.state.location.search).toBe('?stepId=step_local_setup');
  });
});

describe('onboardingApi.getMyRecommendation', () => {
  it('fetches recommendation endpoint and validates schema', async () => {
    const mockRec: Recommendation = {
      state: 'action_available',
      step_id: 'step_1',
      title: 'Шаг 1',
      period: '1 день',
      reason: 'Рекомендуется начать с этого шага',
      action_type: 'confirm',
      action_label: 'Подтвердить',
      target: {
        step_id: 'step_1',
        route: '/onboarding?stepId=step_1',
        action: 'open_card',
      },
    };

    server.use(
      http.get('*/api/v1/onboarding/recommendation', () => HttpResponse.json(mockRec))
    );

    const controller = new AbortController();
    const result = await onboardingApi.getMyRecommendation(controller.signal);
    expect(result.data).toBeDefined();
    expect(result.data?.state).toBe('action_available');
    expect(result.data?.step_id).toBe('step_1');
    expect(result.data?.title).toBe('Шаг 1');
    expect(result.data?.reason).toBe('Рекомендуется начать с этого шага');
  });
});
