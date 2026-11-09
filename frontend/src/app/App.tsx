import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from 'react-router-dom';
import { router } from './router';
import '@/shared/styles/tokens.css';
import '@/shared/styles/reset.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 5, // 5 minutes
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

export interface AppProps {
  routerOverride?: ReturnType<typeof import('react-router-dom').createMemoryRouter>;
}

export const App: React.FC<AppProps> = ({ routerOverride }) => {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={routerOverride || router} />
    </QueryClientProvider>
  );
};
