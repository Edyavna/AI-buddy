import { expect, it, vi, afterEach } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { routes } from '@/app/router';
import { createMentorMock, type MentorScenario } from '@/mocks/mentor/handlers';
import { mentorApi, mentorKeys } from './api';
import { MentorDocumentViewer, validateMarkdown } from './documents';

afterEach(() => vi.restoreAllMocks());

function start(path = '/mentor/questions', scenario: MentorScenario = 'normal') {
  const mock = createMentorMock(scenario); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  return { ...renderWithQuery(<RouterProvider router={router} />), mock, router };
}
it('filters the queue, opens a direct question and resolves only after explicit confirmation', async () => {
  const { mock } = start();
  fireEvent.change(await screen.findByLabelText('Статус вопроса'), { target: { value: 'resolved' } });
  expect(await screen.findByText('Вопросов пока нет')).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Статус вопроса'), { target: { value: '' } });
  fireEvent.click(await screen.findByRole('link', { name: 'Как запустить сервис локально?' }));
  expect(await screen.findByText('Контекст сообщения')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Подтвердить решение' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Взять в работу' }));
  await waitFor(() => expect(mock.control.questions[0]?.status).toBe('in_progress'));
  fireEvent.change(screen.getByLabelText('Комментарий наставника'), { target: { value: '   ' } });
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Тестовая инструкция команды' }));
  expect(screen.getByRole('button', { name: 'Подтвердить решение' })).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Комментарий наставника'), { target: { value: 'Смотрите инструкцию' } });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Подтвердить решение' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить решение' }));
  expect(await screen.findByText('Решение: Смотрите инструкцию')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Открыть повторно' }));
  expect(await screen.findByLabelText('Комментарий наставника')).toBeInTheDocument();
});
it('refetches a revision conflict without automatically retrying the mutation', async () => {
  const { mock } = start('/mentor/questions/gap_demo_1', 'question_changed');
  fireEvent.click(await screen.findByRole('button', { name: 'Взять в работу' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Данные изменились');
  expect(mock.control.questions[0]?.status).toBe('new');
  fireEvent.click(screen.getByRole('button', { name: 'Взять в работу' }));
  await waitFor(() => expect(mock.control.questions[0]?.status).toBe('in_progress'));
});
it('preserves the same upload after an unknown network result and indexes without resolving', async () => {
  const { mock } = start('/mentor/documents?question_id=gap_demo_1');
  const received: string[] = [];
  vi.spyOn(mentorApi, 'upload').mockImplementation(async body => {
    received.push(String(body.get('upload_id')));
    if (received.length === 1) throw new Error('Не удалось связаться с сервером');
    return { ...mock.control.documents[0]!, id: 'uploaded', title: 'Новая инструкция', status: 'processing' };
  });
  await screen.findByText('Тестовая инструкция команды');
  fireEvent.change(screen.getByLabelText('Название документа'), { target: { value: 'Новая инструкция' } });
  fireEvent.change(screen.getByLabelText('Файл Markdown'), { target: { files: [new File(['# Проверка'], 'guide.md')] } });
  fireEvent.click(screen.getByRole('button', { name: 'Загрузить документ' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось связаться');
  expect(screen.getByLabelText('Название документа')).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Повторить ту же загрузку' }));
  expect(await screen.findByText('Документ принят. Дождитесь завершения индексации.')).toBeInTheDocument();
  expect(received).toHaveLength(2); expect(received[0]).toBe(received[1]);
  expect(mock.control.questions[0]?.status).toBe('new');

});
it('shows failed indexing and offers an explicit retry', async () => {
  const { mock, queryClient } = start('/mentor/documents', 'failed');
  mock.control.documents.push({ ...mock.control.documents[0]!, id: 'failed_doc', status: 'failed', error: 'Сбой' });
  await queryClient.invalidateQueries({ queryKey: mentorKeys.documents });
  await queryClient.invalidateQueries({ queryKey: mentorKeys.documents });
  fireEvent.click(await screen.findByRole('button', { name: 'Повторить индексацию' }));
  await waitFor(() => expect(mock.control.documents[1]?.status).toBe('processing'));
});
it('renders empty documents and request failures with retry', async () => {
  const { queryClient } = start('/mentor/documents', 'empty');
  expect(await screen.findByText('Документов пока нет')).toBeInTheDocument();
  server.use(http.get('*/api/v1/mentor/documents', () => new HttpResponse(null, { status: 503 })));
  await queryClient.invalidateQueries({ queryKey: mentorKeys.documents });
  expect(await screen.findByRole('alert')).toHaveTextContent('503');
  expect(screen.getByRole('button', { name: 'Повторить' })).toBeInTheDocument();
});
it.each([401, 403, 404])('hides a cached question and its actions after %s', async status => {
  const { queryClient } = start('/mentor/questions/gap_demo_1');
  await screen.findByText('Как запустить сервис локально?');
  server.use(http.get('*/api/v1/mentor/questions/:id', () => new HttpResponse(null, { status })));
  await queryClient.invalidateQueries({ queryKey: mentorKeys.question('gap_demo_1') });
  expect(await screen.findByRole('alert')).toHaveTextContent('Данные недоступны');
  expect(screen.queryByText('Как запустить сервис локально?')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('Комментарий наставника')).not.toBeInTheDocument();
});
it.each([401, 403, 404])('hides a cached document title and body after %s including reopening', async status => {
  const mock = createMentorMock(); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const { queryClient, rerender } = renderWithQuery(<MentorDocumentViewer id="mentor_doc_1" onClose={() => {}} />);
  await screen.findByRole('heading', { name: 'Локальный запуск' });
  rerender(<></>);
  server.use(http.get('*/api/v1/mentor/documents/:id', () => new HttpResponse(null, { status })));
  rerender(<QueryClientProvider client={queryClient}><MentorDocumentViewer id="mentor_doc_1" onClose={() => {}} /></QueryClientProvider>);
  await queryClient.invalidateQueries({ queryKey: mentorKeys.document('mentor_doc_1') });
  expect(await screen.findByRole('alert')).toHaveTextContent('Данные недоступны');
  expect(screen.queryByText('Тестовая инструкция команды')).not.toBeInTheDocument();
  expect(screen.queryByRole('heading', { name: 'Локальный запуск' })).not.toBeInTheDocument();
  expect(screen.getByRole('dialog')).toHaveAccessibleName('Документ');
});
it('keeps preview keyboard focus contained and Markdown safe', async () => {
  start('/mentor/documents');
  fireEvent.click(await screen.findByRole('button', { name: 'Просмотреть Тестовая инструкция команды' }));
  const dialog = await screen.findByRole('dialog');
  await within(dialog).findByRole('heading', { name: 'Локальный запуск' });
  const close = within(dialog).getByRole('button', { name: 'Закрыть панель' });
  close.focus(); fireEvent.keyDown(close, { key: 'Tab', shiftKey: true });
  expect(within(dialog).getByLabelText('Блок кода')).toHaveFocus();
  fireEvent.keyDown(document.activeElement!, { key: 'Tab' }); expect(close).toHaveFocus();
  fireEvent.keyDown(close, { key: 'Escape' }); expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});
it.each([
  [undefined, 'Title', 'Выберите'], [new File(['text'], 'x.txt'), 'Title', 'Выберите'],
  [new File([' '], 'x.md'), 'Title', 'пустым'], [new File(['x'], 'x.md'), ' ', 'название'],
  [new File([new Uint8Array([255, 254])], 'x.md'), 'Title', 'UTF-8'],
  [new File([new Uint8Array(1048577)], 'x.md'), 'Title', '1 MiB'],
] as const)('validates Markdown input before uploading (%s)', async (file, title, message) => {
  await expect(validateMarkdown(file, title)).rejects.toThrow(message);
});




it('aborts a pending question mutation on logout and prevents a late cache write', async () => {
  const { queryClient, mock } = start('/mentor/questions/gap_demo_1');
  let finish: (() => void) | undefined, receivedSignal: AbortSignal | undefined;
  vi.spyOn(mentorApi, 'updateQuestion').mockImplementation((_id, _body, signal) => {
    receivedSignal = signal;
    return new Promise(resolve => { finish = () => resolve({ ...mock.control.questions[0]!, status: 'in_progress' }); });
  });
  fireEvent.click(await screen.findByRole('button', { name: 'Взять в работу' }));
  await waitFor(() => expect(receivedSignal).toBeDefined());
  fireEvent.click(screen.getByRole('button', { name: 'Выйти' }));
  await screen.findByRole('heading', { name: 'Тестовый вход в демо-режиме' });
  expect(receivedSignal?.aborted).toBe(true);
  finish?.();
  await waitFor(() => expect(queryClient.getQueryData(mentorKeys.question('gap_demo_1'))).toBeUndefined());
});
