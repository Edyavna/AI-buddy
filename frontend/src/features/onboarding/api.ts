import { apiRequest } from '@/shared/api';
import { planSchema, setCheckpointConfirmationSchema, updateStepSchema } from './schemas';
import type { StepTarget } from './schemas';

export type { StepTarget };

export const onboardingApi = {
  getMyPlan(signal: AbortSignal) {
    return apiRequest('/onboarding/plan', planSchema, { signal });
  },

  updateStep(stepId: string, status: StepTarget, signal: AbortSignal) {
    return apiRequest(`/onboarding/steps/${encodeURIComponent(stepId)}`, planSchema, {
      method: 'PATCH',
      body: updateStepSchema.parse({ status }),
      signal,
    });
  },

  setCheckpointConfirmation(stepId: string, confirmed: boolean, revision: number, signal: AbortSignal) {
    return apiRequest(`/onboarding/steps/${encodeURIComponent(stepId)}/confirmation`, planSchema, {
      method: 'PUT',
      body: setCheckpointConfirmationSchema.parse({ confirmed, revision }),
      signal,
    });
  },
};
