import { z } from 'zod';

export const stepTargetSchema = z.enum(['not_started', 'in_progress', 'completed']);
export const checkpointCompletionPolicySchema = z.enum(['intern', 'mentor', 'both']);
export const checkpointActionSchema = z.enum(['confirm', 'revoke']);
export const checkpointRevisionSchema = z.number().int().min(1).max(Number.MAX_SAFE_INTEGER);
export const checkpointConfirmationSchema = z.object({
  role: z.enum(['intern', 'mentor']),
  user_id: z.string(),
  confirmed_at: z.iso.datetime(),
});
export const checkpointResourceSchema = z.object({
  title: z.string().min(1),
  url: z.url().nullable(),
  note: z.string().nullable(),
});

export const stepSchema = z.object({
  id: z.string(),
  title: z.string(),
  description: z.string(),
  status: z.enum(['not_started', 'in_progress', 'blocked', 'completed']),
  request_ids: z.array(z.string()),
  allowed_statuses: z.array(stepTargetSchema),
  blocked_reason: z.string().nullable(),
  template_key: z.string().min(1).optional(),
  period: z.string().nullable().optional(),
  completion_policy: checkpointCompletionPolicySchema.optional(),
  confirmations: z.array(checkpointConfirmationSchema).max(2).optional(),
  revision: checkpointRevisionSchema.optional(),
  allowed_actions: z.array(checkpointActionSchema).optional(),
  awaiting_role: z.enum(['intern', 'mentor']).nullable().optional(),
  instructions: z.string().optional(),
  resources: z.array(checkpointResourceSchema).optional(),
}).transform((step) => ({
  ...step,
  template_key: step.template_key ?? step.id,
  period: step.period ?? null,
  completion_policy: step.completion_policy ?? 'intern',
  confirmations: step.confirmations ?? [],
  revision: step.revision ?? 1,
  allowed_actions: step.allowed_actions ?? [],
  awaiting_role: step.awaiting_role ?? null,
  instructions: step.instructions ?? step.description,
  resources: step.resources ?? [],
}));

export const planSchema = z.object({
  id: z.string(),
  title: z.string(),
  progress_percent: z.number().int().min(0).max(100),
  next_step_id: z.string().nullable(),
  steps: z.array(stepSchema),
});

export const updateStepSchema = z.object({ status: stepTargetSchema }).strict();
export const setCheckpointConfirmationSchema = z.object({
  confirmed: z.boolean(),
  revision: checkpointRevisionSchema,
}).strict();

export type Step = z.infer<typeof stepSchema>;
export type Plan = z.infer<typeof planSchema>;
export type StepTarget = z.infer<typeof stepTargetSchema>;
export type CheckpointCompletionPolicy = z.infer<typeof checkpointCompletionPolicySchema>;
export type CheckpointAction = z.infer<typeof checkpointActionSchema>;
export type CheckpointConfirmation = z.infer<typeof checkpointConfirmationSchema>;
export type CheckpointResource = z.infer<typeof checkpointResourceSchema>;
export type SetCheckpointConfirmation = z.infer<typeof setCheckpointConfirmationSchema>;
