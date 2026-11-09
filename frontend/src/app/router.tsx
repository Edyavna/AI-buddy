import { createBrowserRouter, Navigate, RouteObject } from 'react-router-dom';
import { AppShell } from './AppShell';
import { OnboardingPlaceholderPage } from './routes/OnboardingPlaceholderPage';
import { ChatPlaceholderPage } from './routes/ChatPlaceholderPage';
import { LoginPlaceholderPage } from './routes/LoginPlaceholderPage';

export const routes: RouteObject[] = [
  {
    path: '/login',
    element: <LoginPlaceholderPage />,
  },
  {
    path: '/',
    element: <AppShell />,
    children: [
      {
        index: true,
        element: <Navigate to="/onboarding" replace />,
      },
      {
        path: 'onboarding',
        element: <OnboardingPlaceholderPage />,
      },
      {
        path: 'chat',
        element: <ChatPlaceholderPage />,
      },
      {
        path: 'chat/:conversationId',
        element: <ChatPlaceholderPage />,
      },
      {
        path: '*',
        element: <Navigate to="/onboarding" replace />,
      },
    ],
  },
];

export const router = createBrowserRouter(routes);
