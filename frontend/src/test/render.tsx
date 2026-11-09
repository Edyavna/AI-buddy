import type { ReactElement } from 'react';
import { QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import { afterEach } from 'vitest';
import { createQueryClient } from '../shared/api';

const clients = new Set<ReturnType<typeof createQueryClient>>();
afterEach(() => { clients.forEach(client => client.clear()); clients.clear(); });

export function renderWithQuery(ui: ReactElement) {
  const queryClient = createQueryClient();
  queryClient.setDefaultOptions({ queries: { retry: false }, mutations: { retry: false } });
  clients.add(queryClient);
  return { queryClient, ...render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>) };
}
