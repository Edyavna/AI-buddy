import { createBrowserRouter, Navigate, Outlet, RouteObject } from 'react-router-dom';
import { AppShell } from './AppShell';
import { LoginPage, RequireAuth, useMe } from '@/features/session';
import { PlanPage } from '@/features/onboarding';
import { ChatPage } from '@/features/chat';
import { ResolutionDraftPage } from '@/features/resolution-drafts/ResolutionDraftPage';
import { RoleHome, MentorGate, InternGate, MentorQuestionsPage, MentorQuestionPage, MentorDocumentsPage, MentorInternsPage, MentorInternPlanPage } from '@/features/mentor';

import { HRDocumentsPage, HRQuestionPage, HRQuestionsPage } from '@/features/hr';

function HRGate() {
  const me = useMe();
  return me.data?.role === 'hr' ? <Outlet /> : <RoleHome />;
}

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
        element: <RoleHome />,
      },
      {
        path: 'onboarding',
        element: <InternGate><PlanPage /></InternGate>,
      },
      {
        path: 'chat',
        element: <InternGate><ChatPage /></InternGate>,
      },
      {
        path: 'chat/:conversationId',
        element: <InternGate><ChatPage /></InternGate>,
      },
      { path: 'mentor', element: <MentorGate />, children: [
        { index: true, element: <Navigate to="questions" replace /> },
        { path: 'questions', element: <MentorQuestionsPage /> },
        { path: 'questions/:id', element: <MentorQuestionPage /> },
        { path: 'questions/:id/instruction', element: <ResolutionDraftPage direction="mentor" /> },
        { path: 'documents', element: <MentorDocumentsPage /> },
        { path: 'interns', element: <MentorInternsPage /> },
        { path: 'interns/:id', element: <MentorInternPlanPage /> },
      ] },
      { path: 'hr', element: <HRGate />, children: [
        { index: true, element: <Navigate to="questions" replace /> },
        { path: 'questions', element: <HRQuestionsPage /> },
        { path: 'questions/:id', element: <HRQuestionPage /> },
        { path: 'questions/:id/instruction', element: <ResolutionDraftPage direction="hr" /> },
        { path: 'documents', element: <HRDocumentsPage /> },
      ] },
      { path: '*', element: <RoleHome /> },
    ],
  },
];

export const router = createBrowserRouter(routes);
