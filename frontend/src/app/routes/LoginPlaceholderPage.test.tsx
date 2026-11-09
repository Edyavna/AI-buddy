import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LoginPlaceholderPage } from './LoginPlaceholderPage';

describe('LoginPlaceholderPage', () => {
  it('renders login heading and initial intern selection', () => {
    render(
      <MemoryRouter>
        <LoginPlaceholderPage />
      </MemoryRouter>
    );

    expect(
      screen.getByRole('heading', { level: 1, name: 'Тестовый вход в демо-режиме' })
    ).toBeInTheDocument();

    const radios = screen.getAllByRole('radio');
    expect(radios).toHaveLength(2);

    const internRadio = screen.getByRole('radio', { name: /Иван Стажёров/i });
    const mentorRadio = screen.getByRole('radio', { name: /Алексей Морозов/i });
    expect(internRadio).toHaveAttribute('aria-checked', 'true');
    expect(mentorRadio).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('button', { name: /войти как иван стажёров/i })).toBeInTheDocument();
  });

  it('allows switching account via mouse click', () => {
    render(
      <MemoryRouter>
        <LoginPlaceholderPage />
      </MemoryRouter>
    );

    const internRadio = screen.getByRole('radio', { name: /Иван Стажёров/i });
    const mentorRadio = screen.getByRole('radio', { name: /Алексей Морозов/i });

    // Click mentor
    fireEvent.click(mentorRadio);
    expect(mentorRadio).toHaveAttribute('aria-checked', 'true');
    expect(internRadio).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('button', { name: /войти как алексей морозов/i })).toBeInTheDocument();

    // Click intern
    fireEvent.click(internRadio);
    expect(internRadio).toHaveAttribute('aria-checked', 'true');
    expect(mentorRadio).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('button', { name: /войти как иван стажёров/i })).toBeInTheDocument();
  });

  it('allows switching account via keyboard (Enter and Space)', () => {
    render(
      <MemoryRouter>
        <LoginPlaceholderPage />
      </MemoryRouter>
    );

    const internRadio = screen.getByRole('radio', { name: /Иван Стажёров/i });
    const mentorRadio = screen.getByRole('radio', { name: /Алексей Морозов/i });

    // Press Enter on mentor
    fireEvent.keyDown(mentorRadio, { key: 'Enter' });
    expect(mentorRadio).toHaveAttribute('aria-checked', 'true');

    // Press Space on intern
    fireEvent.keyDown(internRadio, { key: ' ' });
    expect(internRadio).toHaveAttribute('aria-checked', 'true');

    // Other key does not switch
    fireEvent.keyDown(mentorRadio, { key: 'ArrowDown' });
    expect(internRadio).toHaveAttribute('aria-checked', 'true');
  });

  it('renders fallback return button', () => {
    render(
      <MemoryRouter>
        <LoginPlaceholderPage />
      </MemoryRouter>
    );

    expect(
      screen.getByRole('button', { name: /вернуться в систему/i })
    ).toBeInTheDocument();
  });
});
