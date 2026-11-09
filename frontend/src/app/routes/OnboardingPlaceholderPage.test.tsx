import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { OnboardingPlaceholderPage } from './OnboardingPlaceholderPage';

describe('OnboardingPlaceholderPage', () => {
  it('renders onboarding title, steps, mentor info and navigation actions', () => {
    render(
      <MemoryRouter>
        <OnboardingPlaceholderPage />
      </MemoryRouter>
    );

    expect(screen.getByRole('heading', { level: 1, name: 'Мой план адаптации' })).toBeInTheDocument();
    expect(screen.getByText('Прогресс: 0%')).toBeInTheDocument();
    expect(screen.getByText('Шаги онбординга')).toBeInTheDocument();
    expect(screen.getByText('Алексей Морозов')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /перейти в чат/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /написать наставнику/i })).toBeInTheDocument();
  });

  it('opens and closes wiki documentation drawer when clicking wiki link', () => {
    render(
      <MemoryRouter>
        <OnboardingPlaceholderPage />
      </MemoryRouter>
    );

    const wikiButton = screen.getByRole('button', { name: 'Вики команды Backend' });
    fireEvent.click(wikiButton);

    expect(screen.getByRole('heading', { name: 'Вики команды Backend Core' })).toBeInTheDocument();
    expect(screen.getByText(/Добро пожаловать в команду Backend Core!/i)).toBeInTheDocument();

    const closeBtn = screen.getByRole('button', { name: 'Закрыть панель' });
    fireEvent.click(closeBtn);

    expect(screen.queryByRole('heading', { name: 'Вики команды Backend Core' })).not.toBeInTheDocument();
  });

  it('opens and closes git regulations drawer', () => {
    render(
      <MemoryRouter>
        <OnboardingPlaceholderPage />
      </MemoryRouter>
    );

    const gitButton = screen.getByRole('button', { name: 'Регламент Git и CI/CD' });
    fireEvent.click(gitButton);

    expect(screen.getByRole('heading', { name: 'Регламент Git и CI/CD' })).toBeInTheDocument();
    expect(screen.getByText(/Требования к Pull Request/i)).toBeInTheDocument();

    const closeBtn = screen.getByRole('button', { name: 'Закрыть панель' });
    fireEvent.click(closeBtn);

    expect(screen.queryByRole('heading', { name: 'Регламент Git и CI/CD' })).not.toBeInTheDocument();
  });

  it('opens and closes access service documentation drawer', () => {
    render(
      <MemoryRouter>
        <OnboardingPlaceholderPage />
      </MemoryRouter>
    );

    const accessButton = screen.getByRole('button', { name: 'Сервис доступов' });
    fireEvent.click(accessButton);

    expect(screen.getByRole('heading', { name: 'Сервис управления доступами' })).toBeInTheDocument();
    expect(screen.getByText(/Регламентные сроки \(SLA\)/i)).toBeInTheDocument();

    const closeBtn = screen.getByRole('button', { name: 'Закрыть панель' });
    fireEvent.click(closeBtn);

    expect(screen.queryByRole('heading', { name: 'Сервис управления доступами' })).not.toBeInTheDocument();
  });
});
