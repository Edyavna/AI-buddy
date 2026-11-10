import { createBrowserRouter, Navigate, RouteObject } from 'react-router-dom';
import { AppShell } from './AppShell';
import { LoginPage, RequireAuth } from '@/features/session';
import { PlanPage } from '@/features/onboarding';
import { ChatPage } from '@/features/chat';

export const routes: RouteObject[] = [
  {
    path: '/login',
    element: <LoginPage />,
  },
  {
    path: '/',
    element: (
      <RequireAuth>
        <AppShell />
      </RequireAuth>
    ),
    children: [
      {
        index: true,
        element: <Navigate to="/onboarding" replace />,
      },
      {
        path: 'onboarding',
        element: <PlanPage />,
      },
      {
        path: 'chat',
        element: <ChatPage />,
      },
      {
        path: 'chat/:conversationId',
        element: <ChatPage />,
      },
      {
        path: '*',
        element: <Navigate to="/onboarding" replace />,
      },
    ],
  },
];

export const router = createBrowserRouter(routes);
