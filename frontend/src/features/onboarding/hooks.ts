import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { onboardingApi } from './api';
import type { StepTarget } from './api';
import type { Plan } from './schemas';

export function usePlan() {
  return useQuery({
    queryKey: queryKeys.plan(),
    queryFn: async ({ signal }) => {
      const { data } = await onboardingApi.getMyPlan(signal);
      if (!data) throw new ClientError('protocol', 'Сервер вернул пустой план');
      return data;
    },
  });
}

export function useUpdateStep() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ stepId, status }: { stepId: string; status: StepTarget }) =>
      onboardingApi.updateStep(stepId, status),
    // The server returns the whole updated plan: progress and next step are server-side facts.
    onSuccess: (response) => {
      if (response.data) {
        queryClient.setQueryData<Plan>(queryKeys.plan(), response.data);
      }
    },
    // 409 and friends: never a local success. Refresh the plan so the user sees
    // the actual allowed transitions and the blocking reason from the server.
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
    },
  });
}
