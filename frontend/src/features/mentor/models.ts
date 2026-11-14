import { z } from 'zod';
import type { components } from '@/shared/api/generated/schema';
import { contextSchema } from '@/features/question-context/api';

type Schemas = components['schemas'];
const assignmentPerson = z.object({ id: z.string().min(1), name: z.string().min(1) });
export const availableInternSchema = assignmentPerson.extend({ team: assignmentPerson }) satisfies z.ZodType<Schemas['AvailableIntern']>;
export const assignmentSchema = z.object({ intern_id: z.string().min(1), mentor_id: z.string().min(1), team_id: z.string().min(1) }) satisfies z.ZodType<Schemas['MentorAssignment']>;
export type AvailableIntern = z.infer<typeof availableInternSchema>;
const person = z.object({ id: z.string(), name: z.string() });
const date = z.iso.datetime();
export const questionStatusSchema = z.enum(['new', 'in_progress', 'resolved']);
export const questionSchema = z.object({
  context_snapshot:contextSchema.optional(),
  id: z.string(), student: person, step_id: z.string().nullable(), message_id: z.string(),
  question: z.string(), context_excerpt: z.string(), status: questionStatusSchema,
  revision: z.number().int().positive(), document_ids: z.array(z.string()), resolution: z.string().nullable(),
  created_at: date, updated_at: date,
}).transform(({context_snapshot,...question}):Schemas['KnowledgeGap']=>context_snapshot?{...question,context_snapshot}:question) satisfies z.ZodType<Schemas['KnowledgeGap']>;
export const mentorDocumentSchema = z.object({
  id: z.string(), title: z.string(), status: z.enum(['processing', 'ready', 'failed']), author: person,
  created_at: date, updated_at: date, error: z.string().nullable(), question_id: z.string().nullable(),
}) satisfies z.ZodType<Schemas['MentorDocument']>;
export const mentorDocumentDetailSchema = mentorDocumentSchema.extend({ content: z.string() }) satisfies z.ZodType<Schemas['MentorDocumentDetail']>;
export const internSchema = person.extend({
  team: person, progress_percent: z.number().int().min(0).max(100), next_step_id: z.string().nullable(),
  completed_steps: z.number().int().nonnegative(), total_steps: z.number().int().nonnegative(),
  blocked_steps: z.number().int().nonnegative(), open_questions: z.number().int().nonnegative().nullable(),
}) satisfies z.ZodType<Schemas['InternSummary']>;
export type Question = z.infer<typeof questionSchema>;
export type MentorDocument = z.infer<typeof mentorDocumentSchema>;
export type QuestionStatus = Question['status'];
export type Intern = z.infer<typeof internSchema>;
