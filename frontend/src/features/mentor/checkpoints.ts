import type { QueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { queryRequestSignal, revokeQueryAccess } from '@/shared/api/query-access';
import type { Plan } from '@/features/onboarding';
import { mentorApi, mentorKeys } from './api';
import { accessDenied } from './common';

export function acceptMentorPlan(client: QueryClient, id: string, incoming: Plan, signal: AbortSignal): Plan {
  signal.throwIfAborted();
  const current = client.getQueryData<Plan | null>(mentorKeys.plan(id));
  if (current?.id === incoming.id && Math.max(0, ...current.steps.map(s => s.revision)) > Math.max(0, ...incoming.steps.map(s => s.revision))) return current;
  client.setQueryData(mentorKeys.plan(id), incoming);
  return incoming;
}
function deny(client: QueryClient, id: string) {
  for (const key of [mentorKeys.plan(id), mentorKeys.interns]) {
    revokeQueryAccess(client, key); client.setQueryData(key, null);
  }
}
async function refreshSummary(client: QueryClient, signal: AbortSignal) {
  signal.throwIfAborted();
  await client.cancelQueries({ queryKey: mentorKeys.interns, exact: true });
  signal.throwIfAborted();
  await client.invalidateQueries({ queryKey: mentorKeys.interns, exact: true });
}
export async function setMentorConfirmation(client: QueryClient, internId: string, mentorId: string, stepId: string, confirmed: boolean, revision: number, owner: AbortSignal): Promise<Plan> {
  const request = queryRequestSignal(client, mentorKeys.plan(internId), owner);
  await client.cancelQueries({ queryKey: mentorKeys.plan(internId), exact: true });
  request.throwIfAborted();
  try {
    let current: Plan;
    try {
      const next = await mentorApi.setConfirmation(internId, stepId, confirmed, revision, AbortSignal.any([request, AbortSignal.timeout(10_000)]));
      current = acceptMentorPlan(client, internId, next, request);
    } catch (error) {
      request.throwIfAborted();
      if (accessDenied(error)) throw error;
      const conflict = error instanceof ClientError && error.status === 409;
      const uncertain = error instanceof ClientError && (error.kind !== 'http' || (error.status ?? 0) >= 500);
      if (!conflict && !uncertain) throw error;
      let next: Plan;
      try { next = await mentorApi.plan(internId, AbortSignal.any([request, AbortSignal.timeout(10_000)])); }
      catch (refreshError) {
        request.throwIfAborted();
        if (accessDenied(refreshError)) throw refreshError;
        throw new ClientError('network', conflict
          ? 'План изменился, но обновить его не удалось. Проверьте состояние перед повтором.'
          : 'Исход действия не подтверждён: не удалось сверить план. Проверьте состояние и повторите действие.', { cause: refreshError });
      }
      current = acceptMentorPlan(client, internId, next, request);
      await refreshSummary(client, request);
      request.throwIfAborted();
      if (conflict) throw new ClientError('http', 'План изменился. Проверьте новые данные и повторите действие.', { status: 409, code: 'CHECKPOINT_REVISION_CONFLICT' });
      const step = current.steps.find(s => s.id === stepId);
      if (!step || step.confirmations.some(c => c.role === 'mentor' && c.user_id === mentorId) !== confirmed) {
        throw new ClientError('network', 'Исход действия не подтверждён. Проверьте состояние и повторите действие.');
      }
      return current;
    }
    await refreshSummary(client, request);
    request.throwIfAborted();
    return current;
  } catch (error) {
    if (!request.aborted && accessDenied(error)) deny(client, internId);
    throw error;
  }
}
