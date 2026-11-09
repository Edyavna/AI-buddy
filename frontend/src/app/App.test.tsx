import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { createMemoryRouter } from 'react-router-dom';
import { App } from './App';
import { routes } from './router';

describe('App root component', () => {
  it('renders root application without crashing', () => {
    const memoryRouter = createMemoryRouter(routes, {
      initialEntries: ['/onboarding'],
    });
    render(<App routerOverride={memoryRouter} />);
    expect(screen.getByText('Авито')).toBeInTheDocument();
    expect(screen.getByText('AI-buddy')).toBeInTheDocument();
  });
});
