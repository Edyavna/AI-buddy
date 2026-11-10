import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { createMemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { App, queryClient } from './App';
import { routes } from './router';
import { createScenario } from '@/mocks/fixtures/scenario';
import { userSchema } from '@/features/session/schemas';
import { createHandlers } from '@/mocks/handlers';
import { server } from '@/test/server';

beforeEach(() => {
  // The app module keeps one QueryClient: isolate tests from each other's cache.
  queryClient.clear();
});

describe('App root component', () => {
  it('renders the onboarding plan for a session backed by MSW', async () => {
    const memoryRouter = createMemoryRouter(routes, { initialEntries: ['/onboarding'] });
    render(<App routerOverride={memoryRouter} />);
    expect(await screen.findByText('Авито')).toBeInTheDocument();
    expect(screen.getByText('AI-buddy')).toBeInTheDocument();
    // Plan data arrives from the shared handlers
    expect(await screen.findByText('Подготовка к первой задаче')).toBeInTheDocument();
    expect(screen.getByText('Запустить проект')).toBeInTheDocument();
  });

  it('redirects protected routes to /login when the session is missing', async () => {
    const f = createScenario();
    const accounts = [userSchema.parse(f.user)];
    server.resetHandlers(...[
      http.get('*/api/v1/me', () => HttpResponse.json(
        { error: { code: 'UNAUTHORIZED', message: 'Сессия не найдена', trace_id: 'trace_mock' } },
        { status: 401 },
      )),
      http.get('*/api/v1/demo/accounts', () => HttpResponse.json(accounts)),
    ], ...createHandlers());
    const memoryRouter = createMemoryRouter(routes, { initialEntries: ['/onboarding'] });
    render(<App routerOverride={memoryRouter} />);
    expect(await screen.findByRole('heading', { name: 'Тестовый вход в демо-режиме' })).toBeInTheDocument();
  });

  it('renders default root application when no routerOverride is passed', async () => {
    render(<App />);
    expect(await screen.findByText('Авито')).toBeInTheDocument();
    expect(screen.getByText('AI-buddy')).toBeInTheDocument();
  });
});
