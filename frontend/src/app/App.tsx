import React from 'react';
import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from 'react-router-dom';
import { router } from './router';
import { createQueryClient } from '@/shared/api';
import '@/shared/styles/tokens.css';
import '@/shared/styles/reset.css';

const queryClient = createQueryClient();

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
