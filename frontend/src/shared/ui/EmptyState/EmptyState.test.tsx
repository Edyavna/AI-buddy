import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { EmptyState } from './EmptyState';

describe('EmptyState component', () => {
  it('renders title, description and action', () => {
    render(
      <EmptyState
        title="Нет активных заявок"
        description="Когда появится заявка на доступ, она отобразится здесь"
        action={<button>Обновить</button>}
      />
    );

    expect(screen.getByText('Нет активных заявок')).toBeInTheDocument();
    expect(
      screen.getByText('Когда появится заявка на доступ, она отобразится здесь')
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /обновить/i })).toBeInTheDocument();
  });
});
