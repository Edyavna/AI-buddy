import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ChatPlaceholderPage } from './ChatPlaceholderPage';

describe('ChatPlaceholderPage', () => {
  it('renders chat heading and default draft badge when no conversationId', () => {
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<ChatPlaceholderPage />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByRole('heading', { level: 1, name: 'Чат' })).toBeInTheDocument();
    expect(screen.getByText('Новый диалог')).toBeInTheDocument();
    expect(screen.getByText('Слот диалога готов к интеграции')).toBeInTheDocument();
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

  it('keeps the initial history position and scrolls after a new message', () => {
    vi.useFakeTimers();
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

      expect(scrollIntoView).not.toHaveBeenCalled();

      fireEvent.change(screen.getByRole('textbox', { name: 'Сообщение ассистенту' }), {
        target: { value: 'Нужен доступ к репозиторию' },
      });
      fireEvent.click(screen.getByRole('button', { name: 'Отправить сообщение' }));

      expect(scrollIntoView).toHaveBeenCalled();
      expect(screen.getByText('Нужен доступ к репозиторию')).toBeInTheDocument();
    } finally {
      vi.clearAllTimers();
      vi.useRealTimers();
      if (originalDescriptor) {
        Object.defineProperty(Element.prototype, 'scrollIntoView', originalDescriptor);
      } else {
        Reflect.deleteProperty(Element.prototype, 'scrollIntoView');
      }
    }
  });
});
