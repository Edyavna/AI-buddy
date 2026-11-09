import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, act } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ChatPlaceholderPage } from './ChatPlaceholderPage';

describe('ChatPlaceholderPage', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it('renders chat heading, bot info, and default draft badge when no conversationId', () => {
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByRole('heading', { level: 1, name: 'Чат' })).toBeInTheDocument();
    expect(screen.getByText('Новый диалог')).toBeInTheDocument();
    expect(screen.getByText('AI-ассистент стажёра')).toBeInTheDocument();
    expect(screen.getByText('В сети')).toBeInTheDocument();
  });

  it('displays step context banner when stepId search param is present', () => {
    render(
      <MemoryRouter initialEntries={['/chat?stepId=step_local_setup']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByText('Контекст шага адаптации:')).toBeInTheDocument();
    expect(screen.getByText('step_local_setup')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /вернуться к плану/i })).toBeInTheDocument();
  });

  it('displays conversationId badge when route has conversationId param', () => {
    render(
      <MemoryRouter initialEntries={['/chat/conv-123']}>
        <Routes>
          <Route path="/chat/:conversationId" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByText('Диалог: conv-123')).toBeInTheDocument();
  });

  it('allows user to type, send message via submit button, and simulates bot reply', () => {
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

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
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

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

  it('handles access request confirmation dialog workflow', () => {
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

    // Initial draft card
    expect(screen.getByText('Черновик')).toBeInTheDocument();
    const openDialogButton = screen.getByRole('button', { name: 'Подтвердить отправку' });
    fireEvent.click(openDialogButton);

    // Dialog is visible
    expect(screen.getByRole('heading', { name: 'Подтверждение заявки на доступ' })).toBeInTheDocument();
    expect(screen.getByText('Алексей Морозов (Lead Backend Engineer)')).toBeInTheDocument();

    // Cancel closes dialog
    const cancelButton = screen.getByRole('button', { name: 'Отмена' });
    fireEvent.click(cancelButton);
    expect(screen.queryByText('Подтверждение заявки на доступ')).not.toBeInTheDocument();
    expect(screen.getByText('Черновик')).toBeInTheDocument();

    // Open again and confirm
    fireEvent.click(screen.getByRole('button', { name: 'Подтвердить отправку' }));
    const confirmButton = screen.getByRole('button', { name: 'Отправить заявку' });
    fireEvent.click(confirmButton);

    // Status updated
    expect(screen.queryByText('Подтверждение заявки на доступ')).not.toBeInTheDocument();
    expect(screen.getByText(/На согласовании \(ACCESS-1001\)/i)).toBeInTheDocument();
    expect(
      screen.getByText(/Заявка ACCESS-1001 отправлена на согласование Алексею Морозову/i)
    ).toBeInTheDocument();
  });

  it('opens and closes source documentation drawer', () => {
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

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
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

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
      render(
        <MemoryRouter initialEntries={['/chat']}>
          <Routes>
            <Route path="/chat" element={<ChatPlaceholderPage />} />
          </Routes>
        </MemoryRouter>
      );

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
