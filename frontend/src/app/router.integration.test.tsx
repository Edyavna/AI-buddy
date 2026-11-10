import { beforeEach, afterEach, expect, it } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { createMemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { App, queryClient } from './App';
import { routes } from './router';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';

beforeEach(() => queryClient.clear());
afterEach(() => queryClient.clear());

it('opens the FE1 chat from the real FE2 plan, preserving step context', async () => {
  const router = createMemoryRouter(routes, { initialEntries: ['/onboarding'] });
  render(<App routerOverride={router} />);
  fireEvent.click(await screen.findByRole('link', { name: /Спросить помощника о шаге/ }));
  expect(await screen.findByRole('heading', { name: 'Помощник стажёра' })).toBeInTheDocument();
  expect(router.state.location.search).toContain('stepId=step_local_setup');
  expect(screen.getByLabelText('Ваш вопрос')).toHaveValue('Помоги выполнить шаг: Запустить проект');
});

it('opens existing conversations through the protected real router and restores history', async () => {
  const fixture = createScenario();
  const router = createMemoryRouter(routes, { initialEntries: ['/chat/conversation_demo_1'] });
  render(<App routerOverride={router} />);
  expect(await screen.findByText(fixture.messages[1]!.text)).toBeInTheDocument();
  expect(screen.getByLabelText('Ваш вопрос')).toBeInTheDocument();
});

it.each(['/onboarding', '/chat', '/chat/conversation_demo_1'])('keeps RequireAuth on %s', async path => {
  server.use(http.get('*/api/v1/me', () => HttpResponse.json({ error: {
    code: 'UNAUTHENTICATED', message: 'Войдите', trace_id: 'router-test',
  } }, { status: 401 })));
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(<App routerOverride={router} />);
  expect(await screen.findByRole('heading', { name: 'Тестовый вход в демо-режиме' })).toBeInTheDocument();
  expect(router.state.location.pathname).toBe('/login');
});
