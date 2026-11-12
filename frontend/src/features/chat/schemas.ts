import { z } from 'zod';
import { apiErrorSchema } from '../../shared/api';

// Boundary schemas for chat and its session lookup.
const date = z.iso.datetime();
export const userSchema = z.object({
  id: z.string(), name: z.string(), role: z.enum(['intern', 'mentor']),
  team: z.object({ id: z.string(), name: z.string() }),
});
export const conversationSchema = z.object({
  id: z.string(), client_conversation_id: z.uuid(), title: z.string(),
  step_id: z.string().nullable(), created_at: date, updated_at: date,
});
const source = z.object({ id: z.string(), document_id: z.string(), title: z.string(), excerpt: z.string() });
export const messageSchema = z.object({
  id: z.string(), client_message_id: z.uuid().nullable(), in_reply_to_message_id: z.string().nullable(),
  role: z.enum(['user', 'assistant']), text: z.string(),
  status: z.enum(['generating', 'completed', 'failed', 'interrupted']),
  sources: z.array(source), request_ids: z.array(z.string()), created_at: date,
  outcome: z.enum(['unknown', 'answered', 'needs_clarification', 'knowledge_gap']).optional(),
  error: apiErrorSchema.optional(),
}).superRefine((m, ctx) => {
  if (m.role === 'user' && (m.status !== 'completed' || m.client_message_id === null || m.in_reply_to_message_id !== null))
    ctx.addIssue({ code: 'custom', message: 'Invalid user message' });
  if (m.role === 'assistant' && (m.client_message_id !== null || m.in_reply_to_message_id === null))
    ctx.addIssue({ code: 'custom', message: 'Invalid assistant link' });
  if (['failed', 'interrupted'].includes(m.status) !== Boolean(m.error))
    ctx.addIssue({ code: 'custom', message: 'Invalid error state' });
});
