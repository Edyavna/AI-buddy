import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { sessionSignal } from '@/shared/api/session-signal';
import { onboardingApi } from './api';
import type { StepTarget } from './api';
import { planSchema } from './schemas';
import { acceptPlan, handlePlanAccessError } from './cache';
import { setCheckpointConfirmationShared } from './confirm';

const ACTIVE_PLAN_POLL_MS = 5000;

export function usePlan() {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: queryKeys.plan(),
    queryFn: async ({ signal }) => {
      const session = sessionSignal(queryClient);
      try {
        const { data } = await onboardingApi.getMyPlan(AbortSignal.any([signal, session]));
        if (!data) throw new ClientError('protocol', 'Сервер вернул пустой план');
        session.throwIfAborted();
        return acceptPlan(queryClient, data);
      } catch (error) {
        handlePlanAccessError(queryClient, error);
        throw error;
      }
    },
    select: (data) => data === null ? null : planSchema.parse(data),
    refetchInterval: (query) => {
      const plan = query.state.data;
      return plan && plan.progress_percent < 100 ? ACTIVE_PLAN_POLL_MS : false;
    },
    refetchOnWindowFocus: 'always',
  });
}

export function useUpdateStep() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ stepId, status }: { stepId: string; status: StepTarget }) =>
      onboardingApi.updateStep(stepId, status, sessionSignal(queryClient)),
    onSuccess: (response) => {
      if (response.data) acceptPlan(queryClient, response.data);
    },
    onError: (error) => {
      if (!handlePlanAccessError(queryClient, error)) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
      }
    },
  });
}

export function useCheckpointConfirmation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ stepId, confirmed, revision }: { stepId: string; confirmed: boolean; revision: number }) =>
      setCheckpointConfirmationShared(queryClient, stepId, confirmed, revision),
  });
}
