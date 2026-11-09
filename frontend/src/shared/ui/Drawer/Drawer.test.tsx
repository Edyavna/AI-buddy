import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Drawer } from './Drawer';

describe('Drawer component', () => {
  it('does not render when isOpen is false', () => {
    render(
      <Drawer isOpen={false} onClose={vi.fn()} title="Тест">
        Контент
      </Drawer>
    );
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('renders content and title when isOpen is true', () => {
    render(
      <Drawer isOpen={true} onClose={vi.fn()} title="Просмотр документа">
        <p>Текст регламента</p>
      </Drawer>
    );
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Просмотр документа')).toBeInTheDocument();
    expect(screen.getByText('Текст регламента')).toBeInTheDocument();
  });

  it('calls onClose when close button is clicked', async () => {
    const handleClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Drawer isOpen={true} onClose={handleClose} title="Тест">
        Контент
      </Drawer>
    );

    await user.click(screen.getByRole('button', { name: /закрыть панель/i }));
    expect(handleClose).toHaveBeenCalledTimes(1);
  });

  it('calls onClose when Escape key is pressed', () => {
    const handleClose = vi.fn();
    render(
      <Drawer isOpen={true} onClose={handleClose} title="Тест">
        Контент
      </Drawer>
    );

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(handleClose).toHaveBeenCalledTimes(1);
  });

  it('calls onClose when overlay is clicked', async () => {
    const handleClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Drawer isOpen={true} onClose={handleClose} title="Тест">
        Контент
      </Drawer>
    );

    await user.click(screen.getByTestId('drawer-overlay'));
    expect(handleClose).toHaveBeenCalledTimes(1);
  });
});
