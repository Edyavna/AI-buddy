import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AppShell } from './AppShell';
import { renderWithQuery } from '@/test/render';
import { createScenario } from '@/mocks/fixtures/scenario';
import { userSchema } from '@/features/session/schemas';

describe('AppShell layout', () => {
  it('renders Avito branding, navigation links and the current user with logout', async () => {
    const user = userSchema.parse(createScenario().user);
    renderWithQuery(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    );

    // Branding
    expect(screen.getByText('Авито')).toBeInTheDocument();
    expect(screen.getByText('AI-buddy')).toBeInTheDocument();

    // Current user comes from the shared me query
    expect(await screen.findByText(user.name)).toBeInTheDocument();
    expect(screen.getByText(`Стажёр, ${user.team.name}`)).toBeInTheDocument();

    // Navigation links
    expect(screen.getByRole('link', { name: /мой план/i })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /^чат$/i })).toBeInTheDocument();

    // Logout triggers the session cleanup, not a plain link
    expect(screen.getByRole('button', { name: /выйти/i })).toBeInTheDocument();
  });
});
