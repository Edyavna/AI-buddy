import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { TextField } from './TextField';

describe('TextField component', () => {
  it('renders with label and helper text', () => {
    render(
      <TextField
        label="Имя пользователя"
        helperText="Укажите ваше имя для отображения"
        placeholder="Иван"
      />
    );

    expect(screen.getByLabelText('Имя пользователя')).toBeInTheDocument();
    expect(screen.getByText('Укажите ваше имя для отображения')).toBeInTheDocument();
  });

  it('renders error message and marks input as aria-invalid', () => {
    render(
      <TextField
        label="Email"
        errorText="Некорректный email адрес"
      />
    );

    const input = screen.getByLabelText('Email');
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('alert')).toHaveTextContent('Некорректный email адрес');
  });

  it('allows user typing when enabled', async () => {
    const user = userEvent.setup();
    render(<TextField label="Поиск" />);

    const input = screen.getByLabelText('Поиск');
    await user.type(input, 'Привет');
    expect(input).toHaveValue('Привет');
  });
});
