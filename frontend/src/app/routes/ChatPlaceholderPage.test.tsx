import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, screen, act } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { renderWithQuery } from '@/test/render';
import { ChatPlaceholderPage } from './ChatPlaceholderPage';

function renderChat(route: string, path = '/chat') {
  return renderWithQuery(
    <MemoryRouter initialEntries={[route]}>
      <Routes>
        <Route path={path} element={<ChatPlaceholderPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('ChatPlaceholderPage', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it('renders chat heading, bot info, and default draft badge when no conversationId', () => {
    renderChat('/chat');

    expect(screen.getByRole('heading', { level: 1, name: 'Чат' })).toBeInTheDocument();
    expect(screen.getByText('Новый диалог')).toBeInTheDocument();
    expect(screen.getByText('AI-ассистент стажёра')).toBeInTheDocument();
    expect(screen.getByText('В сети')).toBeInTheDocument();
  });

  it('displays step context banner when stepId search param is present', () => {
    renderChat('/chat?stepId=step_local_setup');

    expect(screen.getByText('Контекст шага адаптации:')).toBeInTheDocument();
    expect(screen.getByText('step_local_setup')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /вернуться к плану/i })).toBeInTheDocument();
  });

  it('displays conversationId badge when route has conversationId param', () => {
    renderChat('/chat/conv-123', '/chat/:conversationId');

    expect(screen.getByText('Диалог: conv-123')).toBeInTheDocument();
  });

  it('allows user to type, send message via submit button, and simulates bot reply', () => {
    renderChat('/chat');

    const input = screen.getByRole('textbox', { name: 'Сообщение ассистенту' });
    const sendButton = screen.getByRole('button', { name: 'Отправить сообщение' });

    // Try sending empty text (does nothing)
    fireEvent.change(input, { target: { value: '   ' } });
    fireEvent.click(sendButton);
    expect(screen.queryByText('   ', { selector: 'p' })).not.toBeInTheDocument();

    // Type a real message and send
    fireEvent.change(input, { target: { value: 'Как настроить docker-compose?' } });
    fireEvent.click(sendButton);

    expect(screen.getByText('Как настроить docker-compose?')).toBeInTheDocument();
    expect(input).toHaveValue('');

    // Advance timer for bot simulation
    act(() => {
      vi.advanceTimersByTime(600);
    });

    expect(
      screen.getByText(/Принято! Обрабатываю ваш вопрос по шагу адаптации/i)
    ).toBeInTheDocument();
  });

  it('sends message on Enter key without shift and ignores Shift+Enter', () => {
    renderChat('/chat');

    const input = screen.getByRole('textbox', { name: 'Сообщение ассистенту' });

    // Shift+Enter should NOT send
    fireEvent.change(input, { target: { value: 'Строка 1' } });
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });
    expect(screen.queryByText('Строка 1', { selector: 'p' })).not.toBeInTheDocument();

    // Enter without Shift should send
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: false });
    expect(screen.getByText('Строка 1', { selector: 'p' })).toBeInTheDocument();
    expect(input).toHaveValue('');
  });

  // Единый AccessRequestPanel из плана (integration.md §4): данные — через общий
  // кеш и GET, подтверждение — кнопкой карточки, локальных копий состояния нет.
  it('renders the shared access request panel bound to the saved request', async () => {
    renderChat('/chat');

    expect(await screen.findByText('Черновик')).toBeInTheDocument();
    expect(screen.getByText('Тестовый репозиторий')).toBeInTheDocument();
    const confirmButton = screen.getByRole('button', { name: 'Подтвердить отправку' });

    // Явное подтверждение: сервер принимает заявку (202), карточка уходит в отправку.
    fireEvent.click(confirmButton);
    expect(await screen.findByText('Отправляем заявку / уточняем результат')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Подтвердить отправку' })).not.toBeInTheDocument();
  });

  it('opens and closes source documentation drawer', () => {
    renderChat('/chat');

    const sourceChip = screen.getByRole('button', { name: 'local_setup.md, стр. 4' });
    fireEvent.click(sourceChip);

    expect(screen.getByText(/local_setup\.md \(стр\. 4\) — Доступы к репозиториям/i)).toBeInTheDocument();
    expect(screen.getByText(/Порядок получения доступа/i)).toBeInTheDocument();

    // Close drawer
    const closeBtn = screen.getByRole('button', { name: 'Закрыть панель' });
    fireEvent.click(closeBtn);
    expect(screen.queryByText(/local_setup\.md \(стр\. 4\) — Доступы к репозиториям/i)).not.toBeInTheDocument();
  });

  it('toggles mobile sidebar and closes on backdrop click', () => {
    renderChat('/chat');

    const toggleBtn = screen.getByLabelText(/открыть список диалогов/i);
    fireEvent.click(toggleBtn);

    // Backdrop should exist and close sidebar on click
    const backdrop = document.querySelector('[class*="mobileBackdrop"]');
    expect(backdrop).toBeInTheDocument();
    if (backdrop) {
      fireEvent.click(backdrop);
    }
    expect(document.querySelector('[class*="mobileBackdrop"]')).not.toBeInTheDocument();
  });

  it('handles autoscroll when messages are added', () => {
    const scrollIntoView = vi.fn();
    const originalDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView');
    Object.defineProperty(Element.prototype, 'scrollIntoView', {
      configurable: true,
      writable: true,
      value: scrollIntoView,
    });

    try {
      renderChat('/chat');

      const input = screen.getByRole('textbox', { name: 'Сообщение ассистенту' });
      fireEvent.change(input, { target: { value: 'Тест скролла' } });
      fireEvent.click(screen.getByRole('button', { name: 'Отправить сообщение' }));

      expect(scrollIntoView).toHaveBeenCalled();
    } finally {
      if (originalDescriptor) {
        Object.defineProperty(Element.prototype, 'scrollIntoView', originalDescriptor);
      } else {
        Reflect.deleteProperty(Element.prototype, 'scrollIntoView');
      }
    }
  });
});
