import { z } from 'zod';

// Runtime schema for AccessRequest over HTTP and chat events (integration.md §2, §4).
export const accessRequestSchema = z.object({
  id: z.string(),
  revision: z.number().int().min(1),
  draft_version: z.number().int().min(1),
  conversation_id: z.string(),
  step_id: z.string().nullable(),
  resource: z.object({
    id: z.string(),
    name: z.string(),
    permission: z.enum(['read', 'write']),
  }),
  reason: z.string(),
  status: z.enum(['draft', 'submitting', 'pending', 'granted', 'rejected', 'failed']),
  external_id: z.string().nullable(),
  status_checked_at: z.iso.datetime().nullable(),
  failure_reason: z.string().optional(),
  created_at: z.iso.datetime(),
  updated_at: z.iso.datetime(),
});

export type AccessRequest = z.infer<typeof accessRequestSchema>;
