import { expect, it } from 'vitest';
import { server } from '@/test/server';
import { apiRequest } from '@/shared/api';
import { userSchema, planSchema } from '@/features/chat/schemas';
import { createPreviewHandlers } from './preview';

it('provides explicit synthetic preview context through the real API client', async () => {
  server.use(...createPreviewHandlers());
  expect((await apiRequest('/me', userSchema)).data?.id).toBe('intern_demo_1');
  expect((await apiRequest('/onboarding/plan', planSchema)).data?.steps[0]?.id).toBe('step_local_setup');
});
