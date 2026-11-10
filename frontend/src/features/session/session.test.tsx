import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { queryKeys } from '@/shared/api';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createHandlers } from '@/mocks/handlers';
import { createScenario } from '@/mocks/fixtures/scenario';
import { userSchema } from '@/features/session/schemas';
import { routes } from '@/app/router';

function renderRoute(initialPath: string) {
  const memoryRouter = createMemoryRouter(routes, { initialEntries: [initialPath] });
  const rendered = renderWithQuery(<RouterProvider router={memoryRouter} />);
  return { ...rendered, memoryRouter };
}

describe('demo login flow (#4)', () => {
  it('lists demo accounts, logs in and lands on the onboarding plan', async () => {
    const user = userEvent.setup();
    // No active session: the login form must be offered
    server.resetHandlers(...[
      http.get('*/api/v1/me', () => HttpResponse.json(
        { error: { code: 'UNAUTHORIZED', message: 'Сессия не найдена', trace_id: 'trace_mock' } },
        { status: 401 },
      )),
    ], ...createHandlers());
    renderRoute('/login');

    const mentorCard = await screen.findByRole('radio', { name: /алексей морозов/i });
    expect(screen.getByRole('radio', { name: /тестовый стажер/i })).toBeInTheDocument();

    await user.click(mentorCard);
    await user.click(screen.getByRole('button', { name: /войти как алексей морозов/i }));

    // Navigated to the plan with a session-backed me
    expect(await screen.findByText('Подготовка к первой задаче')).toBeInTheDocument();
    expect(screen.getByText('Алексей Морозов')).toBeInTheDocument();
    expect(screen.getByText(/наставник/i)).toBeInTheDocument();
  });

  it('logs out, clears the query cache and session drafts, and shows the login page', async () => {
    const user = userEvent.setup();
    window.sessionStorage.setItem('draft', 'must-be-cleared');
    const { queryClient } = renderRoute('/onboarding');
    expect(await screen.findByText('Подготовка к первой задаче')).toBeInTheDocument();
    expect(queryClient.getQueryData(queryKeys.me())).toBeDefined();

    await user.click(screen.getByRole('button', { name: /выйти/i }));

    expect(await screen.findByRole('heading', { name: 'Тестовый вход в демо-режиме' })).toBeInTheDocument();
    await waitFor(() => expect(queryClient.getQueryData(queryKeys.me())).toBeUndefined());
    expect(queryClient.getQueryData(queryKeys.plan())).toBeUndefined();
    expect(window.sessionStorage.getItem('draft')).toBeNull();
  });

  it('switches accounts without leaking the previous user cache', async () => {
    const user = userEvent.setup();
    const { queryClient } = renderRoute('/onboarding');
    expect(await screen.findByText('Подготовка к первой задаче')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /выйти/i }));
    expect(await screen.findByRole('heading', { name: 'Тестовый вход в демо-режиме' })).toBeInTheDocument();
    expect(queryClient.getQueryData(queryKeys.me())).toBeUndefined();

    await user.click(await screen.findByRole('radio', { name: /алексей морозов/i }));
    await user.click(screen.getByRole('button', { name: /войти как алексей морозов/i }));
    expect(await screen.findByText('Алексей Морозов')).toBeInTheDocument();
    const me = queryClient.getQueryData<{ name: string }>(queryKeys.me());
    expect(me?.name).toBe('Алексей Морозов');
  });

  it('shows a retryable error instead of logging out when /me fails with 503', async () => {
    server.resetHandlers(...[
      http.get('*/api/v1/me', () => HttpResponse.json(
        { error: { code: 'UNAVAILABLE', message: 'Сервис временно недоступен', trace_id: 'trace_mock' } },
        { status: 503 },
      )),
    ], ...createHandlers());
    const { memoryRouter } = renderRoute('/onboarding');

    expect(await screen.findByRole('alert')).toHaveTextContent('Сервис временно недоступен');
    expect(screen.getByRole('button', { name: /повторить/i })).toBeInTheDocument();
    // No redirect to /login: a network/server failure is not a missing session
    expect(memoryRouter.state.location.pathname).toBe('/onboarding');
    expect(screen.queryByRole('heading', { name: 'Тестовый вход в демо-режиме' })).not.toBeInTheDocument();
  });

  it('redirects to /login on 401 and offers demo accounts again', async () => {
    server.resetHandlers(...[
      http.get('*/api/v1/me', () => HttpResponse.json(
        { error: { code: 'UNAUTHORIZED', message: 'Сессия не найдена', trace_id: 'trace_mock' } },
        { status: 401 },
      )),
      http.get('*/api/v1/demo/accounts', () => {
        const f = createScenario();
        return HttpResponse.json([userSchema.parse(f.user)]);
      }),
    ], ...createHandlers());
    const { memoryRouter } = renderRoute('/onboarding');

    expect(await screen.findByRole('heading', { name: 'Тестовый вход в демо-режиме' })).toBeInTheDocument();
    expect(memoryRouter.state.location.pathname).toBe('/login');
    expect(await screen.findByRole('radio', { name: /тестовый стажер/i })).toBeInTheDocument();
  });
});
