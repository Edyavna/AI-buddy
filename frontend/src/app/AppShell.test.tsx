import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AppShell } from './AppShell';

describe('AppShell layout', () => {
  it('renders Avito branding, navigation links and user profile', () => {
    render(
      <MemoryRouter>
        <AppShell userName="Алексей Смирнов" userRole="Стажёр Frontend" />
      </MemoryRouter>
    );

    // Branding
    expect(screen.getByText('Авито')).toBeInTheDocument();
    expect(screen.getByText('AI-buddy')).toBeInTheDocument();

    // Navigation links
    expect(screen.getByRole('link', { name: /мой план/i })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /чат с наставником/i })).toBeInTheDocument();

    // User section
    expect(screen.getByText('Алексей Смирнов')).toBeInTheDocument();
    expect(screen.getByText('Стажёр Frontend')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /сменить/i })).toHaveAttribute('href', '/login');
  });
});
