import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { InlineError } from './InlineError';

describe('InlineError component', () => {
  it('renders error message in alert role', () => {
    render(<InlineError message="Сервис временно недоступен" />);
    const alert = screen.getByRole('alert');
    expect(alert).toBeInTheDocument();
    expect(screen.getByText('Сервис временно недоступен')).toBeInTheDocument();
  });

  it('renders and invokes retry button when onRetry is provided', async () => {
    const handleRetry = vi.fn();
    const user = userEvent.setup();
    render(<InlineError message="Ошибка сети" onRetry={handleRetry} />);

    const retryButton = screen.getByRole('button', { name: /повторить/i });
    expect(retryButton).toBeInTheDocument();

    await user.click(retryButton);
    expect(handleRetry).toHaveBeenCalledTimes(1);
  });
});
