import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { OnboardingPlaceholderPage } from './OnboardingPlaceholderPage';

describe('OnboardingPlaceholderPage', () => {
  it('renders onboarding title and navigation action', () => {
    render(
      <MemoryRouter>
        <OnboardingPlaceholderPage />
      </MemoryRouter>
    );

    expect(screen.getByRole('heading', { level: 1, name: 'Мой план адаптации' })).toBeInTheDocument();
    expect(screen.getByText('Прогресс: 0%')).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /перейти в чат по первому шагу/i })
    ).toBeInTheDocument();
  });
});
