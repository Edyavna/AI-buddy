import { expect, it } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { createScenario } from '@/mocks/fixtures/scenario';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { ChatPage } from './ChatPage';

it('restores a mentor draft and downloads the text of each completed version', async () => {
  const fixture = createScenario();
  const { conversation, messages } = fixture;
  const assistant = messages[1]!;
  server.use(
    http.get('*/api/v1/me', () => HttpResponse.json({ ...fixture.user, role: 'mentor' })),
    http.get('*/api/v1/conversations/:id', () => HttpResponse.json({ ...conversation, step_id: null, markdown: true })),
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([
      messages[0],
      { ...assistant, text: '# Версия 1', outcome: 'answered' },
      { ...assistant, id: 'clarification', text: 'Какой репозиторий?', outcome: 'needs_clarification' },
      { ...assistant, id: 'revision', text: '# Версия 2\n\nОбновлено', outcome: 'answered' },
    ])),
  );
  renderWithQuery(
    <MemoryRouter initialEntries={[`/mentor/chat/${conversation.id}`]}>
      <Routes><Route path="/mentor/chat/:conversationId?" element={<ChatPage mentor />} /></Routes>
    </MemoryRouter>,
  );
  expect(await screen.findByRole('heading', { name: 'Подготовка Markdown-инструкции' })).toBeInTheDocument();
  expect(await screen.findByText('Какой репозиторий?')).toBeInTheDocument();
  const downloads = screen.getAllByRole('link', { name: 'Скачать .md' });
  expect(downloads.map(link => link.getAttribute('download'))).toEqual(['instruction.md', 'instruction.md']);
  expect(downloads.map(link => decodeURIComponent(link.getAttribute('href')!.split(',')[1]!))).toEqual([
    '# Версия 1', '# Версия 2\n\nОбновлено',
  ]);
  expect(screen.queryByRole('link', { name: 'Открыть мой план' })).not.toBeInTheDocument();
});

it('clears unsent text when switching between new chats without a saved draft', async () => {
  const fixture = createScenario();
  server.use(http.get('*/api/v1/me', () => HttpResponse.json({ ...fixture.user, role: 'mentor' })));
  renderWithQuery(
    <MemoryRouter initialEntries={['/mentor/chat']}>
      <Link to="/mentor/chat?mode=markdown">Открыть Markdown</Link>
      <Routes><Route path="/mentor/chat/:conversationId?" element={<ChatPage mentor />} /></Routes>
    </MemoryRouter>,
  );
  const input = screen.getByRole('textbox', { name: 'Ваш вопрос' });
  await screen.findByRole('link', { name: fixture.conversation.title });
  fireEvent.change(input, { target: { value: 'Обычный вопрос' } });
  fireEvent.click(screen.getByRole('link', { name: 'Открыть Markdown' }));
  await screen.findByRole('heading', { name: 'Подготовка Markdown-инструкции' });
  await waitFor(() => expect(input).toHaveValue(''));
  fireEvent.change(input, { target: { value: 'Текст для инструкции' } });
  fireEvent.click(screen.getByRole('link', { name: 'Новый диалог' }));
  await screen.findByRole('heading', { name: 'Помощник наставника' });
  await waitFor(() => expect(input).toHaveValue(''));
});
