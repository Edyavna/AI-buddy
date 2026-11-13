import { z } from 'zod';

export const reviewTemplateItemSchema = z.object({
  key: z.string(),
  label: z.string(),
  required: z.boolean(),
}).strict();

export const reviewChecklistTemplateSchema = z.object({
  items: z.array(reviewTemplateItemSchema),
  revision: z.number().int(),
  updated_at: z.string(),
  updated_by: z.string(),
  custom: z.boolean(),
}).strict();

export const reviewItemAnswerSchema = z.object({
  done: z.boolean(),
  note: z.string(),
}).strict();

export const reviewAnswersSchema = z.object({
  summary: z.string(),
  verification: z.string(),
  limitations: z.string(),
  questions: z.string(),
  items: z.record(z.string(), reviewItemAnswerSchema),
}).strict();

export const reviewPRSnapshotSchema = z.object({
  url: z.string(),
  repo: z.string(),
  number: z.number().int(),
  title: z.string(),
  head_sha: z.string(),
  source: z.enum(['github', 'manual']),
  fetch_error: z.string(),
  fetched_at: z.string().nullable(),
}).strict();

export const reviewCheckSchema = z.object({
  id: z.string(),
  name: z.string(),
  status: z.enum(['passed', 'failed', 'skipped']),
  source: z.enum(['intern', 'ci']),
  detail: z.string(),
  created_at: z.string(),
}).strict();

export const reviewHistoryEntrySchema = z.object({
  revision: z.number().int(),
  submitted_at: z.string(),
}).strict();

export const reviewPreparationSchema = z.object({
  id: z.string(),
  status: z.enum(['draft', 'submitted']),
  revision: z.number().int(),
  step_id: z.string().nullable(),
  pr: reviewPRSnapshotSchema,
  description: z.string(),
  answers: reviewAnswersSchema,
  checks: z.array(reviewCheckSchema),
  history: z.array(reviewHistoryEntrySchema),
  created_at: z.string(),
  updated_at: z.string(),
  submitted_at: z.string().nullable(),
}).strict();

export const reviewListItemSchema = reviewPreparationSchema.extend({
  student: z.object({ id: z.string(), name: z.string() }).strict(),
}).strict();

export const reviewAssistantResultSchema = z.object({
  summary: z.string(),
  missing: z.array(z.string()),
}).strict();

export const reviewRefreshResultSchema = z.object({
  preparation: reviewPreparationSchema,
  changed: z.boolean(),
}).strict();

export const reviewCheckUpsertSchema = z.object({
  name: z.string(),
  status: z.enum(['passed', 'failed', 'skipped']),
  detail: z.string(),
}).strict();

export const reviewPreparationUpsertSchema = z.object({
  revision: z.number().int().min(1),
  description: z.string(),
  answers: reviewAnswersSchema,
  checks: z.array(reviewCheckUpsertSchema),
}).strict();

export type ReviewTemplateItem = z.infer<typeof reviewTemplateItemSchema>;
export type ReviewChecklistTemplate = z.infer<typeof reviewChecklistTemplateSchema>;
export type ReviewAnswers = z.infer<typeof reviewAnswersSchema>;
export type ReviewCheck = z.infer<typeof reviewCheckSchema>;
export type ReviewPreparation = z.infer<typeof reviewPreparationSchema>;
export type ReviewListItem = z.infer<typeof reviewListItemSchema>;
export type ReviewAssistantResult = z.infer<typeof reviewAssistantResultSchema>;
export type ReviewPreparationUpsert = z.infer<typeof reviewPreparationUpsertSchema>;
