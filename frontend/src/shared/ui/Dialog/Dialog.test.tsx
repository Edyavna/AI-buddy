import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Dialog } from './Dialog';

describe('Dialog component', () => {
  it('does not render when isOpen is false', () => {
    render(
      <Dialog isOpen={false} onClose={vi.fn()} title="Заголовок">
        Контент
      </Dialog>
    );
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('renders title, children and footer when isOpen is true', () => {
    render(
      <Dialog
        isOpen={true}
        onClose={vi.fn()}
        title="Подтверждение действия"
        footer={<button>Подтвердить</button>}
      >
        <p>Вы уверены?</p>
      </Dialog>
    );

    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Подтверждение действия')).toBeInTheDocument();
    expect(screen.getByText('Вы уверены?')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /подтвердить/i })).toBeInTheDocument();
  });

  it('calls onClose when close button is clicked', async () => {
    const handleClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Dialog isOpen={true} onClose={handleClose} title="Окно">
        Контент
      </Dialog>
    );

    await user.click(screen.getByRole('button', { name: /закрыть окно/i }));
    expect(handleClose).toHaveBeenCalledTimes(1);
  });

  it('calls onClose when Escape key is pressed', () => {
    const handleClose = vi.fn();
    render(
      <Dialog isOpen={true} onClose={handleClose} title="Окно">
        Контент
      </Dialog>
    );

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(handleClose).toHaveBeenCalledTimes(1);
  });

  it('calls onClose when backdrop overlay is clicked', async () => {
    const handleClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Dialog isOpen={true} onClose={handleClose} title="Окно">
        Контент
      </Dialog>
    );

    await user.click(screen.getByTestId('dialog-overlay'));
    expect(handleClose).toHaveBeenCalledTimes(1);
  });
});
