import { z } from 'zod';
// Isolated draft shapes from mentor-workspace.md, NOT an agreed/generated API.
const person = z.object({ id: z.string(), name: z.string() });
const date = z.iso.datetime();
export const questionStatusSchema = z.enum(['new', 'in_progress', 'resolved']);
export const questionSchema = z.object({
  id: z.string(), student: person, step_id: z.string().nullable(), message_id: z.string(),
  question: z.string(), context_excerpt: z.string(), status: questionStatusSchema,
  revision: z.number().int().positive(), document_ids: z.array(z.string()), resolution: z.string().nullable(),
  created_at: date, updated_at: date,
});
export const mentorDocumentSchema = z.object({
  id: z.string(), title: z.string(), status: z.enum(['processing', 'ready', 'failed']), author: person,
  created_at: date, updated_at: date, error: z.string().nullable(),
});
export const mentorDocumentDetailSchema = mentorDocumentSchema.extend({ content: z.string() });
export const internSchema = person.extend({
  team: person, progress_percent: z.number().int().min(0).max(100), next_step_id: z.string().nullable(),
  completed_steps: z.number().int().nonnegative(), total_steps: z.number().int().nonnegative(),
  blocked_steps: z.number().int().nonnegative(), open_questions: z.number().int().nonnegative(),
});
export type Question = z.infer<typeof questionSchema>;
export type MentorDocument = z.infer<typeof mentorDocumentSchema>;
export type QuestionStatus = Question['status'];
export type Intern = z.infer<typeof internSchema>;
