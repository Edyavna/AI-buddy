import type { QueryClient } from '@tanstack/react-query';
import { cancelMentorQuestionRequests } from '@/features/mentor-questions';

// FE2 owns session cleanup (integration.md §3): on logout, account switch or 401
// we abort active queries, drop every cache and remove session drafts.
// FE1 keeps its composer draft in sessionStorage (integration.md §1) and deletes it on exit;
// a full clear also guarantees nothing leaks between demo accounts.
export async function resetSessionState(queryClient: QueryClient): Promise<void> {
  cancelMentorQuestionRequests();
  await queryClient.cancelQueries();
  queryClient.clear();
  if (typeof window !== 'undefined') {
    window.sessionStorage.clear();
  }
}
