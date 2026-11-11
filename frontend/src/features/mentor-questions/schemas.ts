import { z } from 'zod';

export const mentorQuestionSchema = z.object({
  id: z.uuid(),
  message_id: z.string(),
  status: z.enum(['new', 'in_progress', 'resolved']),
  revision: z.number().int().min(1),
  resolution: z.string().nullable(),
  document_ids: z.array(z.string()),
  created_at: z.iso.datetime(),
  updated_at: z.iso.datetime(),
});

export const mentorQuestionLookupSchema = z.object({
  question: mentorQuestionSchema.nullable(),
});

export type MentorQuestion = z.infer<typeof mentorQuestionSchema>;
export type MentorQuestionLookup = z.infer<typeof mentorQuestionLookupSchema>;
