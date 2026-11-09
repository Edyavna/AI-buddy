import { QueryClient } from '@tanstack/react-query';
import { ClientError } from './errors';

// FE2 calls once in app/providers. Tests get a fresh isolated instance.
export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (count, error) => count < 2 && error instanceof ClientError &&
          (error.kind === 'network' || (error.kind === 'http' && (error.status ?? 0) >= 500)),
      },
      mutations: { retry: false },
    },
  });
}
