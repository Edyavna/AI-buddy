import { expect, it, vi, afterEach } from 'vitest';
import { act, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { routes } from '@/app/router';
import { resetSessionState } from '@/features/session';
import { createMentorMock, type MentorScenario } from '@/mocks/mentor/handlers';
import { mentorApi, mentorKeys } from './api';

afterEach(() => vi.restoreAllMocks());
function start(scenario: MentorScenario = 'normal') {
  const mock = createMentorMock(scenario); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const router = createMemoryRouter(routes, { initialEntries: ['/mentor/interns'] });
  return { ...renderWithQuery(<RouterProvider router={router} />), mock, router };
}
async function open() {
  fireEvent.click(await screen.findByRole('button', { name: 'Добавить стажёра' }));
  return screen.getByRole('dialog', { name: 'Добавить стажёра' });
}
async function choose(dialog: HTMLElement) {
  fireEvent.click(await within(dialog).findByRole('radio', { name: /Свободный стажёр/ }));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Подтвердить назначение' }));
}
it('searches locally and assigns only after explicit confirmation, refreshing both lists', async () => {
  const { mock } = start(); const spy = vi.spyOn(mentorApi, 'assignIntern');
  const dialog = await open();
  expect(within(dialog).getByRole('button', { name: 'Подтвердить назначение' })).toBeDisabled();
  await within(dialog).findByRole('radio', { name: /Свободный стажёр/ });
  fireEvent.change(within(dialog).getByLabelText('Поиск по имени'), { target: { value: 'Никого' } });
  expect(within(dialog).getByText('Никого не нашли')).toBeInTheDocument();
  fireEvent.change(within(dialog).getByLabelText('Поиск по имени'), { target: { value: 'СВОБОДНЫЙ' } });
  fireEvent.click(within(dialog).getByRole('radio', { name: /Свободный стажёр/ }));
  expect(spy).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Подтвердить назначение' }));
  expect(await screen.findByText('Стажёр Свободный стажёр назначен вам.')).toBeInTheDocument();
  expect(spy).toHaveBeenCalledTimes(1); expect(mock.control.available).toHaveLength(0);
  expect(screen.getByRole('link', { name: 'Свободный стажёр' })).toBeInTheDocument();
  const reopened = await open(); expect(await within(reopened).findByText('Свободных стажёров пока нет')).toBeInTheDocument();
});
it('handles 409 without success or disclosure of another mentor', async () => {
  start('assignment_conflict'); const dialog = await open(); await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Стажёра уже назначили');
  expect(screen.queryByText('Стажёр Свободный стажёр назначен вам.')).not.toBeInTheDocument();
  expect(within(dialog).queryByRole('radio')).not.toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'Подтвердить назначение' })).toBeDisabled();
});
it('confirms a lost PUT response with a fresh assigned list', async () => {
  const { mock } = start('assignment_lost'); const dialog = await open(); await choose(dialog);
  expect(await screen.findByText('Стажёр Свободный стажёр назначен вам.')).toBeInTheDocument();
  expect(mock.control.interns.filter(i => i.id === 'intern_demo_unassigned')).toHaveLength(1);
});
it('preserves an unconfirmed target and retries only by explicit action', async () => {
  start('assignment_unconfirmed'); const spy = vi.spyOn(mentorApi, 'assignIntern');
  const dialog = await open(); await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('пока не подтверждён');
  expect(spy).toHaveBeenCalledTimes(1);
  expect(within(dialog).getByLabelText('Поиск по имени')).toBeDisabled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Повторить назначение' }));
  await waitFor(() => expect(spy).toHaveBeenCalledTimes(2));
  expect(spy.mock.calls[0]?.[0]).toBe('intern_demo_unassigned'); expect(spy.mock.calls[1]?.[0]).toBe('intern_demo_unassigned');
  expect(screen.queryByText('Стажёр Свободный стажёр назначен вам.')).not.toBeInTheDocument();
});
it('does not treat stale assigned data as confirmation when reconciliation fails', async () => {
  start('assignment_unconfirmed'); const dialog = await open();
  await within(dialog).findByRole('radio', { name: /Свободный стажёр/ });
  server.use(http.get('*/api/v1/mentor/interns', () => new HttpResponse(null, { status: 503 })));
  await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('пока не подтверждён');
  expect(within(dialog).getByRole('button', { name: 'Повторить назначение' })).toBeEnabled();
});
it('keeps an assignment successful when the intern has no plan and offers plan retry', async () => {
  start(); const dialog = await open(); await choose(dialog);
  await screen.findByText('Стажёр Свободный стажёр назначен вам.');
  fireEvent.click(screen.getByRole('link', { name: 'Свободный стажёр' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('План недоступен или ещё не подготовлен');
  expect(screen.getByRole('button', { name: 'Повторить' })).toBeInTheDocument();
});
it('shows empty and retryable available list states', async () => {
  start('empty'); const dialog = await open();
  expect(await within(dialog).findByText('Свободных стажёров пока нет')).toBeInTheDocument();
  server.use(http.get('*/api/v1/mentor/available-interns', () => new HttpResponse(null, { status: 503 })));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Отмена' }));
  const again = await open();
  expect(await within(again).findByRole('alert')).toHaveTextContent('503');
  expect(within(again).getByRole('button', { name: 'Повторить' })).toBeInTheDocument();
});
it.each([401, 403, 404])('clears assignment data on PUT %s and keeps it hidden after 503', async status => {
  const { queryClient } = start(); const dialog = await open();
  server.use(http.put('*/api/v1/mentor/interns/:id/assignment', () => new HttpResponse(null, { status })));
  await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Назначение недоступно');
  expect(queryClient.getQueryData(mentorKeys.availableInterns)).toBeNull();
  expect(queryClient.getQueryData(mentorKeys.interns)).toBeNull();
  server.use(http.get('*/api/v1/mentor/available-interns', () => new HttpResponse(null, { status: 503 })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.availableInterns });
  expect(within(dialog).queryByRole('radio')).not.toBeInTheDocument();
  expect(screen.queryByText('Свободный стажёр')).not.toBeInTheDocument();
});
it('ignores list responses that started before assignment access was denied', async () => {
  const { queryClient, mock } = start();
  const dialog = await open();
  await within(dialog).findByRole('radio', { name: /Свободный стажёр/ });

  let release!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  const available = vi.fn(async () => {
    await pending;
    return HttpResponse.json({ items: mock.control.available });
  });
  const assigned = vi.fn(async () => {
    await pending;
    return HttpResponse.json({ items: mock.control.interns });
  });
  server.use(
    http.get('*/api/v1/mentor/available-interns', available),
    http.get('*/api/v1/mentor/interns', assigned),
  );
  const refresh = Promise.all([
    queryClient.refetchQueries({ queryKey: mentorKeys.availableInterns }),
    queryClient.refetchQueries({ queryKey: mentorKeys.interns }),
  ]);
  await waitFor(() => {
    expect(available).toHaveBeenCalled();
    expect(assigned).toHaveBeenCalled();
  });

  server.use(http.put('*/api/v1/mentor/interns/:id/assignment', () => new HttpResponse(null, { status: 403 })));
  await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Назначение недоступно');
  await act(async () => {
    release();
    await refresh;
  });
  expect(queryClient.getQueryData(mentorKeys.availableInterns)).toBeNull();
  expect(queryClient.getQueryData(mentorKeys.interns)).toBeNull();
});

it('cancels an assignment on session reset and ignores a late response before unmount', async () => {
  const { queryClient, mock } = start(); let finish: (() => void) | undefined, received: AbortSignal | undefined;
  const spy = vi.spyOn(mentorApi, 'assignIntern').mockImplementation((id, signal) => {
    received = signal; return new Promise(resolve => { finish = () => resolve({ intern_id: id, mentor_id: mock.control.interns[0]!.id, team_id: 'team_demo_backend' }); });
  });
  const dialog = await open(); await choose(dialog);
  const pendingButton = within(dialog).getByText('Подтвердить назначение').closest('button')!;
  expect(pendingButton).toBeDisabled(); fireEvent.click(pendingButton);
  expect(spy).toHaveBeenCalledTimes(1);
  await resetSessionState(queryClient);
  expect(received?.aborted).toBe(true); finish?.();
  await waitFor(() => expect(queryClient.getQueryData(mentorKeys.availableInterns)).toBeUndefined());
  expect(screen.queryByText('Стажёр Свободный стажёр назначен вам.')).not.toBeInTheDocument();
});
it('contains keyboard focus and restores it when closing the picker', async () => {
  start(); const dialog = await open();
  await within(dialog).findByRole('radio', { name: /Свободный стажёр/ });
  const close = within(dialog).getByRole('button', { name: 'Закрыть окно' }); close.focus();
  fireEvent.keyDown(close, { key: 'Tab', shiftKey: true });
  expect(within(dialog).getByRole('button', { name: 'Отмена' })).toHaveFocus();
  fireEvent.keyDown(document.activeElement!, { key: 'Tab' }); expect(close).toHaveFocus();
  fireEvent.keyDown(close, { key: 'Escape' });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});
it('uses PUT without a body and accepts repeated own assignments', async () => {
  const mock = createMentorMock(); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const signal = new AbortController().signal;
  expect((await mentorApi.availableInterns(signal)).items).toHaveLength(1);
  expect((await mentorApi.assignIntern('intern_demo_unassigned', signal)).intern_id).toBe('intern_demo_unassigned');
  expect((await mentorApi.assignIntern('intern_demo_unassigned', signal)).intern_id).toBe('intern_demo_unassigned');
  expect((await mentorApi.availableInterns(signal)).items).toHaveLength(0);
  await expect(mentorApi.assignIntern('foreign', signal)).rejects.toMatchObject({ status: 404 });
});


it('hides an unconfirmed selection after later access revocation and transient errors', async () => {
  const { queryClient } = start('assignment_unconfirmed'); const dialog = await open(); await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('пока не подтверждён');
  server.use(http.get('*/api/v1/mentor/available-interns', () => new HttpResponse(null, { status: 403 })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.availableInterns });
  await waitFor(() => expect(within(dialog).queryByText('Повторить назначение: Свободный стажёр')).not.toBeInTheDocument());
  expect(within(dialog).getByRole('button', { name: 'Повторить назначение' })).toBeDisabled();
  server.use(http.get('*/api/v1/mentor/available-interns', () => new HttpResponse(null, { status: 503 })));
  await queryClient.refetchQueries({ queryKey: mentorKeys.availableInterns });
  expect(within(dialog).queryByText('Повторить назначение: Свободный стажёр')).not.toBeInTheDocument();
});
it('prioritizes access denial over a successful reconciliation list', async () => {
  start('assignment_lost'); const dialog = await open();
  await within(dialog).findByRole('radio', { name: /Свободный стажёр/ });
  server.use(http.get('*/api/v1/mentor/available-interns', () => new HttpResponse(null, { status: 403 })));
  await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Назначение недоступно');
  expect(screen.queryByText('Стажёр Свободный стажёр назначен вам.')).not.toBeInTheDocument();
});

it('refreshes revoked lists explicitly without repeating the denied PUT', async () => {
  const { mock } = start(); const spy = vi.spyOn(mentorApi, 'assignIntern');
  const dialog = await open();
  server.use(http.put('*/api/v1/mentor/interns/:id/assignment', () => new HttpResponse(null, { status: 403 })));
  await choose(dialog);
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Назначение недоступно');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Повторить' }));
  expect(await within(dialog).findByRole('radio', { name: /Свободный стажёр/ })).toBeInTheDocument();
  expect(spy).toHaveBeenCalledTimes(1);
  expect(mock.control.available).toHaveLength(1);
});
