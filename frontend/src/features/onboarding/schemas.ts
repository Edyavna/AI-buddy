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

export const recommendationStateSchema = z.enum([
  'action_available',
  'waiting_mentor',
  'waiting_access',
  'blocked_dependencies',
  'blocked',
  'completed',
  'empty_plan',
]);

export const recommendationActionTypeSchema = z.enum([
  'confirm',
  'read_instructions',
  'assistant',
  'view_step',
]);

export const recommendationTargetSchema = z.object({
  step_id: z.string(),
  route: z.string(),
  action: z.enum(['open_card', 'open_instructions', 'open_chat']),
});

export const waitingStepInfoSchema = z.object({
  step_id: z.string(),
  title: z.string(),
  period: z.string().nullable().optional(),
  reason: z.string(),
  state: recommendationStateSchema,
  awaiting_role: z.enum(['intern', 'mentor']).nullable().optional(),
  help_direction: z.enum(['mentor', 'hr']).nullable().optional(),
  blocked_at: z.string().nullable().optional(),
  waiting_duration_seconds: z.number().int().nullable().optional(),
});

export const recommendationHelpActionSchema = z.object({
  direction: z.enum(['mentor', 'hr']),
  label: z.string(),
  url: z.string(),
  available: z.boolean(),
});

export const recommendationSchema = z.object({
  state: recommendationStateSchema,
  step_id: z.string().nullable().optional(),
  title: z.string().nullable().optional(),
  period: z.string().nullable().optional(),
  reason: z.string(),
  action_type: recommendationActionTypeSchema.nullable().optional(),
  action_label: z.string().nullable().optional(),
  target: recommendationTargetSchema.nullable().optional(),
  waiting_step: waitingStepInfoSchema.nullable().optional(),
  help_action: recommendationHelpActionSchema.nullable().optional(),
});

export const planSchema = z.object({
  id: z.string(),
  title: z.string(),
  progress_percent: z.number().int().min(0).max(100),
  next_step_id: z.string().nullable(),
  recommendation: recommendationSchema.optional(),
  steps: z.array(stepSchema),
}).transform((plan) => {
  const steps = plan.steps;
  const nextStep = steps.find((s) => s.id === plan.next_step_id);
  const fallbackRec: Recommendation = plan.recommendation ?? (
    steps.length === 0 ? {
      state: 'empty_plan',
      reason: 'План адаптации пуст',
    } : plan.progress_percent === 100 ? {
      state: 'completed',
      reason: 'Все шаги плана успешно завершены!',
    } : nextStep ? {
      state: 'action_available',
      step_id: nextStep.id,
      title: nextStep.title,
      period: nextStep.period,
      reason: 'Текущий доступный шаг по плану адаптации',
      action_type: 'confirm',
      action_label: 'Подтвердить выполнение',
      target: { step_id: nextStep.id, route: '/onboarding', action: 'open_card' },
    } : {
      state: 'blocked',
      reason: 'Все оставшиеся шаги заблокированы',
    }
  );
  return {
    ...plan,
    recommendation: fallbackRec,
  };
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
export type Recommendation = z.infer<typeof recommendationSchema>;
export type RecommendationState = z.infer<typeof recommendationStateSchema>;
export type RecommendationActionType = z.infer<typeof recommendationActionTypeSchema>;
export type WaitingStepInfo = z.infer<typeof waitingStepInfoSchema>;
export type RecommendationHelpAction = z.infer<typeof recommendationHelpActionSchema>;
export type RecommendationTarget = z.infer<typeof recommendationTargetSchema>;
