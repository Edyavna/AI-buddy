import { z } from 'zod';

// Runtime boundary schemas for onboarding endpoints (OpenAPI is the source of truth).
export const stepTargetSchema = z.enum(['not_started', 'in_progress', 'completed']);

export const stepSchema = z.object({
  id: z.string(),
  title: z.string(),
  description: z.string(),
  status: z.enum(['not_started', 'in_progress', 'blocked', 'completed']),
  request_ids: z.array(z.string()),
  allowed_statuses: z.array(stepTargetSchema),
  blocked_reason: z.string().nullable(),
});

export const planSchema = z.object({
  id: z.string(),
  title: z.string(),
  progress_percent: z.number().int().min(0).max(100),
  next_step_id: z.string().nullable(),
  steps: z.array(stepSchema),
});

export const updateStepSchema = z
  .object({ status: stepTargetSchema })
  .strict();

export type Step = z.infer<typeof stepSchema>;
export type Plan = z.infer<typeof planSchema>;
export type StepTarget = z.infer<typeof stepTargetSchema>;
