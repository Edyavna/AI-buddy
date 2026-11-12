import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { queryRequestSignal } from '@/shared/api/query-access';
import { onboardingApi } from './api';
import type { StepTarget } from './api';
import { planSchema } from './schemas';
import { acceptPlan, handlePlanAccessError } from './cache';
import { setCheckpointConfirmationShared } from './confirm';

const ACTIVE_PLAN_POLL_MS = 5000;

export function usePlan(enabled = true) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: queryKeys.plan(),
    enabled,
    queryFn: async ({ signal }) => {
      const request = queryRequestSignal(queryClient, queryKeys.plan(), signal);
      try {
        const { data } = await onboardingApi.getMyPlan(request);
        if (!data) throw new ClientError('protocol', 'Сервер вернул пустой план');
        return acceptPlan(queryClient, data, request);
      } catch (error) {
        if (!request.aborted) handlePlanAccessError(queryClient, error);
        throw error;
      }
    },
    select: (data) => data === null ? null : planSchema.parse(data),
    refetchInterval: (query) => {
      const plan = query.state.data;
      return plan ? ACTIVE_PLAN_POLL_MS : false;
    },
    refetchOnWindowFocus: 'always',
  });
}

export function useUpdateStep() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ stepId, status }: { stepId: string; status: StepTarget }) => {
      const request = queryRequestSignal(queryClient, queryKeys.plan());
      try {
        const response = await onboardingApi.updateStep(stepId, status, request);
        request.throwIfAborted();
        if (response.data) acceptPlan(queryClient, response.data, request);
        return response;
      } catch (error) {
        if (!request.aborted && !handlePlanAccessError(queryClient, error)) {
          void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
        }
        throw error;
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
