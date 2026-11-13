import { createBrowserRouter, Navigate, RouteObject } from 'react-router-dom';
import { AppShell } from './AppShell';
import { LoginPage, RequireAuth } from '@/features/session';
import { PlanPage } from '@/features/onboarding';
import { ChatPage } from '@/features/chat';
import { RoleHome, MentorGate, InternGate, MentorQuestionsPage, MentorQuestionPage, MentorDocumentsPage, MentorInternsPage, MentorInternPlanPage } from '@/features/mentor';
import { ReviewPreparationPage, MentorReviewPreparationsPage, MentorReviewPreparationPage } from '@/features/review-preparation';

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
      {
        path: 'review-preparation',
        element: <InternGate><ReviewPreparationPage /></InternGate>,
      },
      { path: 'mentor', element: <MentorGate />, children: [
        { index: true, element: <Navigate to="questions" replace /> },
        { path: 'questions', element: <MentorQuestionsPage /> },
        { path: 'questions/:id', element: <MentorQuestionPage /> },
        { path: 'documents', element: <MentorDocumentsPage /> },
        { path: 'interns', element: <MentorInternsPage /> },
        { path: 'interns/:id', element: <MentorInternPlanPage /> },
        { path: 'review-preparations', element: <MentorReviewPreparationsPage /> },
        { path: 'review-preparations/:id', element: <MentorReviewPreparationPage /> },
      ] },
      { path: '*', element: <RoleHome /> },
    ],
  },
];

export const router = createBrowserRouter(routes);
