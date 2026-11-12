import type { QueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { sessionSignal } from '@/shared/api/session-signal';
import { onboardingApi } from './api';
import { acceptPlan, handlePlanAccessError } from './cache';
import type { Plan } from './schemas';

const CONFIRMATION_TIMEOUT_MS = 10_000;
const inflight = new WeakMap<AbortSignal, Map<string, Promise<Plan>>>();

export function setCheckpointConfirmationShared(
  queryClient: QueryClient,
  stepId: string,
  confirmed: boolean,
  revision: number,
): Promise<Plan> {
  const session = sessionSignal(queryClient);
  let operations = inflight.get(session);
  if (!operations) {
    operations = new Map();
    inflight.set(session, operations);
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
          AbortSignal.any([session, timeout]),
        );
        if (!data) throw new ClientError('protocol', 'Сервер вернул пустой план');
        session.throwIfAborted();
        return acceptPlan(queryClient, data);
      } catch (error) {
        session.throwIfAborted();
        if (handlePlanAccessError(queryClient, error)) throw error;

        const revisionConflict = error instanceof ClientError && error.code === 'CHECKPOINT_REVISION_CONFLICT';
        const uncertain = timeout.aborted || error instanceof ClientError &&
          (error.kind === 'network' || error.kind === 'protocol' || (error.status ?? 0) >= 500);
        if (!revisionConflict && !uncertain) {
          void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
          throw error;
        }

        const { data } = await onboardingApi.getMyPlan(
          AbortSignal.any([session, AbortSignal.timeout(CONFIRMATION_TIMEOUT_MS)]),
        );
        if (!data) throw new ClientError('protocol', 'Сервер вернул пустой план');
        session.throwIfAborted();
        const current = acceptPlan(queryClient, data);
        const step = current.steps.find((item) => item.id === stepId);
        const isConfirmed = step?.confirmations.some((item) => item.role === 'intern') ?? false;

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
      handlePlanAccessError(queryClient, error);
      throw error;
    }
  })().finally(() => {
    operations.delete(stepId);
  });

  operations.set(stepId, task);
  return task;
}
