import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { createScenario } from '@/mocks/fixtures/scenario';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { ChatPage } from './ChatPage';
import { ChatController } from './controller';

const fixture = createScenario();
function mount(path: string) {
  server.use(http.get('*/api/v1/me', () => HttpResponse.json({ ...fixture.user, role: 'mentor' })));
  return renderWithQuery(
    <MemoryRouter initialEntries={[path]}>
      <Routes><Route path="/mentor/chat/:conversationId?" element={<ChatPage mentor />} /></Routes>
    </MemoryRouter>,
  );
}

it('resends the original message after a saved model failure', async () => {
  const [question, answer] = fixture.messages;
  server.use(
    http.get('*/api/v1/conversations/:id', () => HttpResponse.json({ ...fixture.conversation, step_id: null })),
    http.get('*/api/v1/conversations/:id/messages', () => HttpResponse.json([
      question,
      { ...answer, status: 'failed', error: { error: { code: 'LLM_UNAVAILABLE', message: 'Модель недоступна', trace_id: 'test' } } },
    ])),
  );
  const send = vi.spyOn(ChatController.prototype, 'send').mockResolvedValue(undefined);
  try {
    mount(`/mentor/chat/${fixture.conversation.id}`);
    fireEvent.click(await screen.findByRole('button', { name: 'Повторить запрос' }));
    expect(send).toHaveBeenCalledWith(question!.text, fixture.user.id, null, expect.any(Function), { newMessage: true });
  } finally {
    send.mockRestore();
  }
});

it('retries creation after the network request fails', async () => {
  let attempts = 0;
  server.use(http.post('*/api/v1/conversations', () => {
    attempts++;
    return HttpResponse.error();
  }));
  mount('/mentor/chat');
  await screen.findByRole('link', { name: fixture.conversation.title });
  fireEvent.change(screen.getByRole('textbox', { name: 'Ваш вопрос' }), { target: { value: 'Вопрос наставника' } });
  fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Повторить исходный вопрос' }));
  await screen.findByRole('button', { name: 'Повторить исходный вопрос' });
  await waitFor(() => expect(attempts).toBe(2));
  sessionStorage.clear();
});
