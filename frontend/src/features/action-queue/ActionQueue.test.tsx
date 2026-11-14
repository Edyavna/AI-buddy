import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { MemoryRouter } from 'react-router-dom';
import { expect, it } from 'vitest';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { abortSessionRequests } from '@/shared/api/session-signal';
import { ActionQueuePage } from './ActionQueuePage';
import { CheckpointInvitation } from './CheckpointInvitation';
import { completeInvitation, queueKey, type Invitation, type QueueItem } from './api';

const api = '*/api/v1';
const invitationPath = `${api}/onboarding/steps/team-step/invitation`;
const mentorPath = `${api}/mentor/interns/one/invitations/:id`;
const since = '2026-11-14T10:30:00Z';
const id = 'ddc7543a-ac50-4750-9408-fb1a349931f5';
const intern = { id: 'one', name: 'Тестовый стажёр' };
const invite: QueueItem = { id, kind: 'invitation', action: 'record_invitation', intern, step_id: 'team-step', step_title: 'Организационные моменты', question_preview: null, since, updated_at: since };
const confirmation: QueueItem = { ...invite, kind: 'confirmation', action: 'review_checkpoint', id: 'team-step' };
const question: QueueItem = { ...invite, id: 'question', kind: 'question', action: 'answer_question', step_id: null, step_title: null, question_preview: 'Как настроить проект команды?', updated_at: '2026-11-15T10:30:00Z' };
const pending: Invitation = { id, state: 'pending', requested_at: since, closed_at: null, note: null };
const completed: Invitation = { ...pending, state: 'completed', closed_at: since, note: 'Добавил в канал' };
const queue = (items: QueueItem[], total = items.length) => ({
  items, total, interns: items.length ? [intern] : [],
  counts: { invitation: 1, confirmation: 1, question: total },
});
const renderQueue = (role: 'mentor' | 'hr' = 'mentor') =>
  renderWithQuery(<MemoryRouter><ActionQueuePage role={role} /></MemoryRouter>);
const json = (value: unknown, status = 200) => HttpResponse.json(value as Record<string, unknown>, { status });

it('shows all sources and filters, pages and returns to the last existing page', async () => {
  let tooFew = false;
  const params: string[][] = [];
  server.use(http.get(`${api}/action-queue`, ({ request }) => {
    const q = new URL(request.url).searchParams;
    params.push([q.get('intern_id') ?? '', q.get('kind') ?? '', q.get('offset') ?? '']);
    if (q.get('kind') === 'question') return json(tooFew ? queue([question], 1) : queue([question], 21));
    return json(queue([invite, confirmation, question]));
  }));
  const { queryClient } = renderQueue();
  expect(screen.getByText('Загружаем…')).toBeInTheDocument();
  await screen.findByText('Стажёр ждёт приглашения в закрытые чаты');
  expect(screen.getAllByRole('link', { name: 'К чекпоинту' })[0]).toHaveAttribute('href', '/mentor/interns/one#step-team-step');
  expect(screen.getByRole('link', { name: 'Открыть вопрос' })).toHaveAttribute('href', '/mentor/questions/question');
  expect(screen.getByText(/Обновлено/)).toBeInTheDocument();
  expect(screen.getByText(question.question_preview!)).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Стажёр'), { target: { value: 'one' } });
  await waitFor(() => expect(params).toContainEqual(['one', '', '0']));
  fireEvent.change(screen.getByLabelText('Тип действия'), { target: { value: 'question' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Далее' }));
  await waitFor(() => expect(params).toContainEqual(['one', 'question', '20']));
  fireEvent.click(await screen.findByRole('button', { name: 'Назад' }));
  await screen.findByText('1–1 из 21');
  fireEvent.click(screen.getByRole('button', { name: 'Далее' }));
  await screen.findByText('21–21 из 21');
  tooFew = true;
  await act(() => queryClient.refetchQueries({ queryKey: queueKey, type: 'active' }));
  await screen.findByRole('link', { name: 'Открыть вопрос' });
  await waitFor(() => expect(screen.queryByRole('navigation', { name: 'Страницы очереди' })).not.toBeInTheDocument());
});

it('links HR to the current operation and recovers a failed list load', async () => {
  let outage = true;
  server.use(http.get(`${api}/action-queue`, () => outage
    ? json({}, 503)
    : json(queue([{ ...question, action: 'claim_question' }]))));
  renderQueue('hr');
  await screen.findByRole('alert');
  outage = false;
  fireEvent.click(screen.getByRole('button', { name: /Повторить/ }));
  expect(await screen.findByRole('link', { name: 'Взять обращение' })).toHaveAttribute('href', '/hr/questions/question');
  expect(within(screen.getByLabelText('Тип действия')).queryByRole('option', { name: 'Приглашения' })).not.toBeInTheDocument();
});

it('verifies a lost completion response before moving keyboard focus and dropping the card', async () => {
  let saved = false;
  let puts = 0;
  server.use(
    http.get(`${api}/action-queue`, () => json(queue(saved ? [] : [invite]))),
    http.put(mentorPath, async ({ request }) => {
      expect(await request.json()).toEqual({ note: completed.note });
      puts++;
      saved = true;
      return new HttpResponse(null, { status: 503 });
    }),
    http.get(mentorPath, () => json(completed)),
  );
  renderQueue();
  fireEvent.change(await screen.findByRole('textbox', { name: /Пояснение к приглашению/ }), { target: { value: completed.note } });
  const submit = screen.getByRole('button', { name: 'Отметить ручное приглашение' });
  fireEvent.click(submit);
  fireEvent.click(submit);
  expect(await screen.findByText('Ручное приглашение отмечено.')).toBeInTheDocument();
  expect(screen.getByRole('heading', { level: 1 })).toHaveFocus();
  expect(await screen.findByText('Нет ожидающих действий')).toBeInTheDocument();
  expect(puts).toBe(1);
});

it('drops denied queue data and keeps it hidden on the next service failure', async () => {
  let status = 200;
  server.use(http.get(`${api}/action-queue`, () => json(status === 200 ? queue([invite]) : {}, status)));
  const { queryClient } = renderQueue();
  await screen.findByRole('heading', { name: intern.name });
  status = 403;
  await act(() => queryClient.refetchQueries({ queryKey: queueKey }));
  await screen.findByRole('alert');
  expect(screen.queryByRole('heading', { name: intern.name })).not.toBeInTheDocument();
  status = 503;
  fireEvent.click(screen.getByRole('button', { name: /Повторить/ }));
  await screen.findByRole('alert');
  expect(screen.queryByRole('heading', { name: intern.name })).not.toBeInTheDocument();
});

it('discards a completion that arrives after the session was cleared', async () => {
  let release!: () => void;
  server.use(
    http.get(`${api}/action-queue`, () => json(queue([invite]))),
    http.put(mentorPath, async () => {
      await new Promise<void>(resolve => { release = resolve; });
      return json(completed);
    }),
  );
  const { queryClient } = renderQueue();
  fireEvent.click(await screen.findByRole('button', { name: 'Отметить ручное приглашение' }));
  await waitFor(() => expect(release).toBeDefined());
  await act(async () => { abortSessionRequests(queryClient); release(); });
  expect(screen.queryByText('Ручное приглашение отмечено.')).not.toBeInTheDocument();
});

it('requests, cancels and shows a completed manual invitation in the intern plan', async () => {
  let state: Invitation | null = null;
  server.use(
    http.get(invitationPath, () => json({ invitation: state })),
    http.put(invitationPath, async ({ request }) => {
      const change = await request.json() as { id: string; action: string };
      state = { ...pending, id: change.id, state: change.action === 'request' ? 'pending' : 'canceled' };
      return json({ invitation: state });
    }),
  );
  const { queryClient } = renderWithQuery(<CheckpointInvitation step="team-step" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Попросить приглашение' }));
  expect(await screen.findByText('Наставник получил запрос на ручное приглашение.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Отменить запрос' }));
  await screen.findByRole('button', { name: 'Попросить приглашение' });
  state = completed;
  await act(() => queryClient.refetchQueries({ queryKey: ['checkpoint-invitation'] }));
  expect(await screen.findByText(/Наставник отметил, что пригласил вас вручную/)).toHaveTextContent(completed.note!);
  expect(screen.getByRole('button', { name: 'Запросить ещё одно приглашение' })).toBeInTheDocument();
});

it('keeps the request on an HTTP conflict and refuses to infer a completed write from an unconfirmed read', async () => {
  server.use(
    http.get(invitationPath, () => json({ invitation: pending })),
    http.put(invitationPath, () => json({}, 409)),
    http.put(mentorPath, () => json({}, 503)),
    http.get(mentorPath, () => json(pending)),
  );
  renderWithQuery(<CheckpointInvitation step="team-step" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Отменить запрос' }));
  await screen.findByRole('alert');
  expect(await screen.findByRole('button', { name: 'Отменить запрос' })).toBeInTheDocument();
  await expect(completeInvitation('one', id, '', new AbortController().signal)).rejects.toThrow('Действие не подтверждено');
});
