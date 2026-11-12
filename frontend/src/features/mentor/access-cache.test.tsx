import { expect, it } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createMentorMock } from '@/mocks/mentor/handlers';
import { createMentorFixtures } from '@/mocks/mentor/fixtures';
import { mentorKeys } from './api';
import { MentorInternsPage } from './pages';

it.each([401, 403, 404])('keeps mentor data revoked after %s followed by transient errors until a successful refresh', async status => {
  const mock = createMentorMock();
  mock.control.setRole('mentor');
  server.use(...mock.handlers);
  const { queryClient, rerender } = renderWithQuery(<MemoryRouter><MentorInternsPage /></MemoryRouter>);
  await screen.findByText('Тестовый стажер');
  expect(queryClient.getQueryData(mentorKeys.interns)).toMatchObject({ items: [{ name: 'Тестовый стажер' }] });

  server.use(http.get('*/api/v1/mentor/interns', () => new HttpResponse(null, { status })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.interns, exact: true });
  await waitFor(() => expect(screen.queryByText('Тестовый стажер')).not.toBeInTheDocument());
  expect(queryClient.getQueryData(mentorKeys.interns)).toBeNull();

  server.use(http.get('*/api/v1/mentor/interns', () => new HttpResponse(null, { status: 503 })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.interns, exact: true });
  expect(await screen.findByRole('alert')).toHaveTextContent('503');
  expect(screen.queryByText('Тестовый стажер')).not.toBeInTheDocument();
  expect(screen.queryByText('Прогресс: 0% · 0 из 1 шагов')).not.toBeInTheDocument();
  expect(queryClient.getQueryData(mentorKeys.interns)).toBeNull();

  // Leaving/reopening must retain revocation in the shared cache, not component state.
  rerender(<QueryClientProvider client={queryClient}><></></QueryClientProvider>);
  rerender(<QueryClientProvider client={queryClient}><MemoryRouter><MentorInternsPage /></MemoryRouter></QueryClientProvider>);
  expect(await screen.findByRole('alert')).toHaveTextContent('503');
  expect(screen.queryByText('Тестовый стажер')).not.toBeInTheDocument();

  server.use(http.get('*/api/v1/mentor/interns', () => HttpResponse.error()));
  await queryClient.refetchQueries({ queryKey: mentorKeys.interns, exact: true });
  expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось связаться');
  expect(screen.queryByText('Тестовый стажер')).not.toBeInTheDocument();
  expect(queryClient.getQueryData(mentorKeys.interns)).toBeNull();

  const fresh = createMentorFixtures().interns;
  fresh[0]!.name = 'Обновлённый стажёр';
  server.use(http.get('*/api/v1/mentor/interns', () => HttpResponse.json({ items: fresh })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.interns, exact: true });
  expect(await screen.findByText('Обновлённый стажёр')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(queryClient.getQueryData(mentorKeys.interns)).toEqual({ items: fresh });
});

it('preserves last successful mentor data on transient failures without prior access denial', async () => {
  const mock = createMentorMock();
  mock.control.setRole('mentor');
  server.use(...mock.handlers);
  const { queryClient } = renderWithQuery(<MemoryRouter><MentorInternsPage /></MemoryRouter>);
  await screen.findByText('Тестовый стажер');
  const cached = queryClient.getQueryData(mentorKeys.interns);

  for (const response of [() => new HttpResponse(null, { status: 503 }), () => HttpResponse.error()]) {
    server.use(http.get('*/api/v1/mentor/interns', response));
    await queryClient.refetchQueries({ queryKey: mentorKeys.interns, exact: true });
    await screen.findByRole('alert');
    expect(screen.getByText('Тестовый стажер')).toBeInTheDocument();
    expect(queryClient.getQueryData(mentorKeys.interns)).toEqual(cached);
  }
});
