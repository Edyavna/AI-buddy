import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import type { RequestOptions } from '@/shared/api';
import {
  reviewAssistantResultSchema,
  reviewChecklistTemplateSchema,
  reviewListItemSchema,
  reviewPreparationSchema,
  type ReviewPreparationUpsert,
  reviewPreparationUpsertSchema,
  reviewRefreshResultSchema,
  reviewTemplateItemSchema,
} from './schemas';

export const reviewPrepKeys = {
  template: ['review-preparation', 'template'] as const,
  latest: ['review-preparation', 'latest'] as const,
  mentorList: ['review-preparation', 'mentor-list'] as const,
  mentorItem: (id: string) => ['review-preparation', 'mentor', id] as const,
};

async function request<T>(path: string, schema: z.ZodType<T>, options: RequestOptions = {}) {
  const result = await apiRequest(path, schema, options);
  if (!result.data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
  return result.data;
}

const segment = encodeURIComponent;

export const reviewPrepApi = {
  template: (signal: AbortSignal) => request('/review-preparations/template', reviewChecklistTemplateSchema, { signal }),
  latest: (signal: AbortSignal) => request('/review-preparations', z.object({ item: reviewPreparationSchema.nullable() }), { signal }),
  create: (prUrl: string, manual: boolean, signal: AbortSignal) =>
    request('/review-preparations', reviewPreparationSchema, { method: 'POST', body: { pr_url: prUrl, step_id: null, manual }, signal }),
  update: (id: string, body: ReviewPreparationUpsert, signal: AbortSignal) =>
    request(`/review-preparations/${segment(id)}`, reviewPreparationSchema, {
      method: 'PUT', body: reviewPreparationUpsertSchema.parse(body), signal,
    }),
  submit: (id: string, revision: number, signal: AbortSignal) =>
    request(`/review-preparations/${segment(id)}/submit`, reviewPreparationSchema, { method: 'POST', body: { revision }, signal }),
  assistant: (id: string, signal: AbortSignal) =>
    request(`/review-preparations/${segment(id)}/assistant`, reviewAssistantResultSchema, { method: 'POST', signal }),
  refresh: (id: string, signal: AbortSignal) =>
    request(`/review-preparations/${segment(id)}/refresh`, reviewRefreshResultSchema, { method: 'POST', signal }),
  saveTemplate: (items: z.infer<typeof reviewTemplateItemSchema>[], revision: number, signal: AbortSignal) =>
    request('/review-preparations/template', reviewChecklistTemplateSchema, { method: 'PUT', body: { items, revision }, signal }),
  mentorList: (signal: AbortSignal) => request('/mentor/review-preparations', z.object({ items: z.array(reviewListItemSchema) }), { signal }),
  mentorItem: (id: string, signal: AbortSignal) => request(`/mentor/review-preparations/${segment(id)}`, reviewPreparationSchema, { signal }),
};
