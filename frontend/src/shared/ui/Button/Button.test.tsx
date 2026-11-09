import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Button } from './Button';

describe('Button component', () => {
  it('renders children correctly', () => {
    render(<Button>Нажми меня</Button>);
    expect(screen.getByRole('button', { name: /нажми меня/i })).toBeInTheDocument();
  });

  it('handles click events when enabled', async () => {
    const handleClick = vi.fn();
    const user = userEvent.setup();
    render(<Button onClick={handleClick}>Действие</Button>);

    await user.click(screen.getByRole('button', { name: /действие/i }));
    expect(handleClick).toHaveBeenCalledTimes(1);
  });

  it('does not trigger click when disabled', async () => {
    const handleClick = vi.fn();
    const user = userEvent.setup();
    render(<Button disabled onClick={handleClick}>Заблокировано</Button>);

    await user.click(screen.getByRole('button', { name: /заблокировано/i }));
    expect(handleClick).not.toHaveBeenCalled();
    expect(screen.getByRole('button')).toBeDisabled();
  });

  it('shows loading spinner and is disabled when isLoading is true', async () => {
    const handleClick = vi.fn();
    const user = userEvent.setup();
    render(<Button isLoading onClick={handleClick}>Сохранить</Button>);

    const button = screen.getByRole('button');
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByRole('status')).toBeInTheDocument();

    await user.click(button);
    expect(handleClick).not.toHaveBeenCalled();
  });
});
