import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LoginPlaceholderPage } from './LoginPlaceholderPage';

describe('LoginPlaceholderPage', () => {
  it('renders login heading and return link button', () => {
    render(
      <MemoryRouter>
        <LoginPlaceholderPage />
      </MemoryRouter>
    );

    expect(
      screen.getByRole('heading', { level: 1, name: 'Тестовый вход в демо-режиме' })
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /вернуться в систему/i })
    ).toBeInTheDocument();
  });
});
