import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AvitoLogo } from './AvitoLogo';

describe('AvitoLogo', () => {
  it('renders official Avito logo with accessible name', () => {
    render(<AvitoLogo />);
    expect(screen.getByText('Авито')).toBeInTheDocument();
  });
});
