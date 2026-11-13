import type { QueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { sessionSignal } from '@/shared/api/session-signal';
import { queryAccessSignal } from '@/shared/api/query-access';
import { onboardingApi } from './api';
import { acceptPlan, handlePlanAccessError, isPlanAccessError } from './cache';
import type { Plan } from './schemas';

const CONFIRMATION_TIMEOUT_MS = 10_000;
const inflight = new WeakMap<AbortSignal, WeakMap<AbortSignal, Map<string, Promise<Plan>>>>();

export function setCheckpointConfirmationShared(
  queryClient: QueryClient,
  stepId: string,
  confirmed: boolean,
  revision: number,
): Promise<Plan> {
  const session = sessionSignal(queryClient);
  const access = queryAccessSignal(queryClient, queryKeys.plan());
  const request = AbortSignal.any([session, access]);
  let scopes = inflight.get(session);
  if (!scopes) { scopes = new WeakMap(); inflight.set(session, scopes); }
  let operations = scopes.get(access);
  if (!operations) {
    operations = new Map();
    scopes.set(access, operations);
  }
  const pending = operations.get(stepId);
  if (pending) return pending;

  const task = (async () => {
    const timeout = AbortSignal.timeout(CONFIRMATION_TIMEOUT_MS);
    try {
      try {
        const { data } = await onboardingApi.setCheckpointConfirmation(
          stepId,
          confirmed,
          revision,
          AbortSignal.any([request, timeout]),
        );
        if (!data) throw new ClientError('protocol', 'Сервер вернул пустой план');
        return acceptPlan(queryClient, data, request);
      } catch (error) {
        request.throwIfAborted();
        if (isPlanAccessError(error)) throw error;

        const revisionConflict = error instanceof ClientError && error.code === 'CHECKPOINT_REVISION_CONFLICT';
        const uncertain = timeout.aborted || error instanceof ClientError &&
          (error.kind === 'network' || error.kind === 'protocol' || (error.status ?? 0) >= 500);
        if (!revisionConflict && !uncertain) {
          void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
          throw error;
        }

        let current: Plan;
        try {
          const { data } = await onboardingApi.getMyPlan(
            AbortSignal.any([request, AbortSignal.timeout(CONFIRMATION_TIMEOUT_MS)]),
          );
          if (!data) throw new ClientError('protocol', 'Сервер вернул пустой план');
          current = acceptPlan(queryClient, data, request);
        } catch (refreshError) {
          request.throwIfAborted();
          if (isPlanAccessError(refreshError)) throw refreshError;
          throw new ClientError('network', revisionConflict
            ? 'План изменился, но обновить его не удалось. Проверьте состояние перед повтором.'
            : 'Исход действия не подтверждён: не удалось сверить план. Проверьте состояние и повторите действие.',
          { cause: refreshError });
        }
        const step = current.steps.find((item) => item.id === stepId);
        const isConfirmed = step?.confirmations.some((item) => item.role === 'intern');

        if (uncertain && isConfirmed === confirmed) return current;
        if (revisionConflict) {
          throw new ClientError('http', 'План изменился. Проверьте актуальное состояние и повторите действие.', {
            status: 409,
            code: 'CHECKPOINT_REVISION_CONFLICT',
            cause: error,
          });
        }
        throw new ClientError(
          'network',
          'Исход действия не подтверждён. Проверьте состояние шага и повторите попытку.',
          { cause: error },
        );
      }
    } catch (error) {
      if (!request.aborted) handlePlanAccessError(queryClient, error);
      throw error;
    }
  })().finally(() => {
    operations.delete(stepId);
  });

  operations.set(stepId, task);
  return task;
}
