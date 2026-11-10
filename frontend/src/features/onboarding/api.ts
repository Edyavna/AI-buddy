import { apiRequest } from '@/shared/api';
import { planSchema, updateStepSchema } from './schemas';
import type { StepTarget } from './schemas';

export type { StepTarget };

export const onboardingApi = {
  getMyPlan(signal: AbortSignal) {
    return apiRequest('/onboarding/plan', planSchema, { signal });
  },

  updateStep(stepId: string, status: StepTarget) {
    return apiRequest(`/onboarding/steps/${encodeURIComponent(stepId)}`, planSchema, {
      method: 'PATCH',
      body: updateStepSchema.parse({ status }),
    });
  },
};
