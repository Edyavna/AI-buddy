import { expect, it } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { routes } from '@/app/router';
import { createMentorMock } from '@/mocks/mentor/handlers';
import { mentorApi, mentorKeys } from './api';
import { createMentorFixtures } from '@/mocks/mentor/fixtures';

it('isolates mentor fixtures and enforces mock role checks', async () => {
  const mock = createMentorMock(); server.use(...mock.handlers);
  const signal = new AbortController().signal;
  await expect(mentorApi.questions('', signal)).rejects.toMatchObject({ status: 401 });
  mock.control.setRole('intern');
  await expect(mentorApi.questions('', signal)).rejects.toMatchObject({ status: 403 });
  mock.control.setRole('mentor');
  expect((await mentorApi.questions('new', signal)).items).toHaveLength(1);
  expect((await mentorApi.questions('resolved', signal)).items).toHaveLength(0);
  expect((await mentorApi.questions('new&status=resolved', signal)).items).toHaveLength(0);
  expect((await mentorApi.question('gap_demo_1', signal)).student.id).toBe('intern_demo_1');
  expect((await mentorApi.documents(signal)).items).toHaveLength(1);
  expect((await mentorApi.document('mentor_doc_1', signal)).content).toContain('Синтетический');
  expect((await mentorApi.interns(signal)).items).toHaveLength(1);
  expect((await mentorApi.plan('intern_demo_1', signal)).progress_percent).toBe(0);
  await expect(mentorApi.plan('foreign', signal)).rejects.toMatchObject({ status: 404 });
  expect(mentorKeys.all).toEqual(['mentor']);
  expect(createMentorFixtures().questions[0]?.revision).toBe(1);
});

it.each(['/mentor/questions', '/mentor/questions/gap_demo_1', '/mentor/documents', '/mentor/interns', '/mentor/interns/intern_demo_1'])('guards mentor URL %s from intern accounts', async path => {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  renderWithQuery(<RouterProvider router={router} />);
  expect(await screen.findByText('Подготовка к первой задаче')).toBeInTheDocument();
  expect(router.state.location.pathname).toBe('/onboarding');
});

it('logs in mentor to the workspace and clears previous account caches on logout', async () => {
  const mock = createMentorMock(); server.use(...mock.handlers);
  const router = createMemoryRouter(routes, { initialEntries: ['/login'] });
  const { queryClient } = renderWithQuery(<RouterProvider router={router} />);
  queryClient.setQueryData(mentorKeys.questions(), { secret: 'old account' });
  fireEvent.click(await screen.findByRole('radio', { name: /Тестовый наставник/ }));
  fireEvent.click(screen.getByRole('button', { name: /Войти как Тестовый наставник/ }));
  expect(await screen.findByRole('heading', { name: 'Вопросы без ответа' })).toBeInTheDocument();
  expect(queryClient.getQueryData(mentorKeys.questions())).not.toEqual({ secret: 'old account' });
  expect(router.state.location.pathname).toBe('/mentor/questions');
  expect(screen.queryByRole('link', { name: 'Мой план' })).not.toBeInTheDocument();
  window.sessionStorage.setItem('mentor-draft', 'test');
  fireEvent.click(screen.getByRole('button', { name: 'Выйти' }));
  expect(await screen.findByRole('heading', { name: 'Тестовый вход в демо-режиме' })).toBeInTheDocument();
  expect(queryClient.getQueryData(mentorKeys.questions())).toBeUndefined();
  expect(window.sessionStorage.length).toBe(0);
});

it('routes mentors away from intern pages without removing those routes', async () => {
  const mock = createMentorMock(); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const router = createMemoryRouter(routes, { initialEntries: ['/chat'] });
  renderWithQuery(<RouterProvider router={router} />);
  expect(await screen.findByRole('heading', { name: 'Вопросы без ответа' })).toBeInTheDocument();
});

