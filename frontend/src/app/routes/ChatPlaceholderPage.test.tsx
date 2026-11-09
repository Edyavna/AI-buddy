import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
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
});
