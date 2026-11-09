import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatusBadge } from './StatusBadge';

describe('StatusBadge component', () => {
  it('renders default text for granted status', () => {
    render(<StatusBadge status="granted" />);
    expect(screen.getByText('Доступ выдан')).toBeInTheDocument();
  });

  it('renders default text for in_progress status', () => {
    render(<StatusBadge status="in_progress" />);
    expect(screen.getByText('В работе')).toBeInTheDocument();
  });

  it('allows custom label override', () => {
    render(<StatusBadge status="draft" label="Новый черновик" />);
    expect(screen.getByText('Новый черновик')).toBeInTheDocument();
  });
});
