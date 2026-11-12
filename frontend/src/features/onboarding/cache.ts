import type { QueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { abortSessionRequests } from '@/shared/api/session-signal';
import { revokeQueryAccess } from '@/shared/api/query-access';
import type { Plan } from './schemas';

export const isPlanAccessError = (error: unknown) =>
  error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);

export function acceptPlan(queryClient: QueryClient, incoming: Plan, signal: AbortSignal): Plan {
  signal.throwIfAborted();
  const key = queryKeys.plan();
  const current = queryClient.getQueryData<Plan | null>(key);
  if (current && current.id === incoming.id) {
    const currentRevision = Math.max(0, ...current.steps.map((step) => step.revision));
    const incomingRevision = Math.max(0, ...incoming.steps.map((step) => step.revision));
    if (incomingRevision < currentRevision) return current;
  }
  queryClient.setQueryData(key, incoming);
  return incoming;
}

export function handlePlanAccessError(queryClient: QueryClient, error: unknown): boolean {
  if (!isPlanAccessError(error)) return false;
  revokeQueryAccess(queryClient, queryKeys.plan());
  if (error instanceof ClientError && error.status === 401) {
    abortSessionRequests(queryClient);
    queryClient.clear();
  } else {
    queryClient.setQueryData<Plan | null>(queryKeys.plan(), null);
  }
  return true;
}
