import { z } from 'zod';
import { apiErrorSchema } from '../../shared/api';

// Test-only contract checks, not production feature schemas. FE2 owns the latter.
const date = z.iso.datetime();
export const userFixtureSchema = z.object({
  id: z.string(), name: z.string(), role: z.enum(['intern', 'mentor', 'hr']),
  team: z.object({ id: z.string(), name: z.string() }),
});
export const conversationFixtureSchema = z.object({
  id: z.string(), client_conversation_id: z.uuid(), title: z.string(),
  step_id: z.string().nullable(), created_at: date, updated_at: date,
});
const source = z.object({ id: z.string(), document_id: z.string(), title: z.string(), excerpt: z.string() });
export const messageFixtureSchema = z.object({
  id: z.string(), client_message_id: z.uuid().nullable(), in_reply_to_message_id: z.string().nullable(),
  role: z.enum(['user', 'assistant']), text: z.string(),
  status: z.enum(['generating', 'completed', 'failed', 'interrupted']),
  sources: z.array(source), request_ids: z.array(z.string()), created_at: date,
  error: apiErrorSchema.optional(),
}).superRefine((m, ctx) => {
  if (m.role === 'user' && (m.status !== 'completed' || m.client_message_id === null || m.in_reply_to_message_id !== null))
    ctx.addIssue({ code: 'custom', message: 'Invalid user message' });
  if (m.role === 'assistant' && (m.client_message_id !== null || m.in_reply_to_message_id === null))
    ctx.addIssue({ code: 'custom', message: 'Invalid assistant link' });
  if (['failed', 'interrupted'].includes(m.status) !== Boolean(m.error))
    ctx.addIssue({ code: 'custom', message: 'Invalid error state' });
});
export const requestFixtureSchema = z.object({
  id: z.string(), revision: z.number().int().min(1), draft_version: z.number().int().min(1),
  conversation_id: z.string(), step_id: z.string().nullable(),
  resource: z.object({ id: z.string(), name: z.string(), permission: z.enum(['read', 'write']) }),
  reason: z.string(), status: z.enum(['draft', 'submitting', 'pending', 'granted', 'rejected', 'failed']),
  external_id: z.string().nullable(), status_checked_at: date.nullable(),
  created_at: date, updated_at: date, failure_reason: z.string().optional(),
}).superRefine((r, ctx) => {
  if (['pending', 'granted', 'rejected'].includes(r.status) && !r.external_id)
    ctx.addIssue({ code: 'custom', message: 'Missing external ID' });
  if (['draft', 'failed'].includes(r.status) && r.external_id !== null)
    ctx.addIssue({ code: 'custom', message: 'Unexpected external ID' });
  if (['failed', 'rejected'].includes(r.status) && !r.failure_reason)
    ctx.addIssue({ code: 'custom', message: 'Missing failure reason' });
});
export const recommendationHelpActionFixtureSchema = z.object({
  direction: z.enum(['mentor', 'hr']),
  label: z.string(),
  url: z.string(),
  available: z.boolean(),
});

export const waitingStepFixtureSchema = z.object({
  step_id: z.string(),
  title: z.string(),
  period: z.string().nullable(),
  reason: z.string(),
  state: z.enum([
    'action_available',
    'waiting_mentor',
    'waiting_access',
    'blocked_dependencies',
    'blocked',
    'completed',
    'empty_plan',
  ]),
  awaiting_role: z.enum(['intern', 'mentor']).nullable(),
  help_direction: z.enum(['mentor', 'hr']).nullable(),
  blocked_at: date.nullable(),
  waiting_duration_seconds: z.number().int().nullable(),
});

export const recommendationTargetFixtureSchema = z.object({
  step_id: z.string(),
  route: z.string(),
  action: z.enum(['open_card', 'open_instructions', 'open_chat']),
});

export const recommendationFixtureSchema = z.object({
  state: z.enum([
    'action_available',
    'waiting_mentor',
    'waiting_access',
    'blocked_dependencies',
    'blocked',
    'completed',
    'empty_plan',
  ]),
  step_id: z.string().nullable(),
  title: z.string().nullable(),
  period: z.string().nullable(),
  reason: z.string(),
  action_type: z.enum(['confirm', 'read_instructions', 'assistant', 'view_step']).nullable(),
  action_label: z.string().nullable(),
  target: recommendationTargetFixtureSchema.nullable(),
  waiting_step: waitingStepFixtureSchema.nullable(),
  help_action: recommendationHelpActionFixtureSchema.nullable(),
});

export const planFixtureSchema = z.object({
  id: z.string(), title: z.string(), progress_percent: z.number().int().min(0).max(100), next_step_id: z.string().nullable(),
  recommendation: recommendationFixtureSchema,
  steps: z.array(z.object({
    id: z.string(), title: z.string(), description: z.string(),
    status: z.enum(['not_started', 'in_progress', 'blocked', 'completed']), request_ids: z.array(z.string()),
    allowed_statuses: z.array(z.enum(['not_started', 'in_progress', 'completed'])), blocked_reason: z.string().nullable(),
    template_key: z.string().min(1), period: z.string().nullable(),
    completion_policy: z.enum(['intern', 'mentor', 'both']),
    confirmations: z.array(z.object({
      role: z.enum(['intern', 'mentor']), user_id: z.string(), confirmed_at: date,
    })).max(2),
    revision: z.number().int().positive(),
    allowed_actions: z.array(z.enum(['confirm', 'revoke'])),
    awaiting_role: z.enum(['intern', 'mentor']).nullable(),
    instructions: z.string(),
    resources: z.array(z.object({ title: z.string().min(1), url: z.url().nullable(), note: z.string().nullable() })),
  })),
});
export const documentFixtureSchema = z.object({ id: z.string(), title: z.string(), content: z.string(), updated_at: date });
