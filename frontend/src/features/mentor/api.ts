import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import type { RequestOptions } from '@/shared/api';
import type { components } from '@/shared/api/generated/schema';
import { planSchema } from '@/features/onboarding';
import { questionSchema, mentorDocumentSchema, mentorDocumentDetailSchema, internSchema } from './models';

export const mentorKeys = {
  all: ['mentor'] as const,
  questions: (status = '') => ['mentor', 'questions', status] as const,
  question: (id: string) => ['mentor', 'question', id] as const,
  documents: ['mentor', 'documents'] as const,
  document: (id: string) => ['mentor', 'document', id] as const,
  interns: ['mentor', 'interns'] as const,
  plan: (id: string) => ['mentor', 'plan', id] as const,
};
async function request<T>(path: string, schema: z.ZodType<T>, options: RequestOptions = {}) {
  const result = await apiRequest(path, schema, options);
  if (!result.data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
  return result.data;
}
const segment = encodeURIComponent;
export type QuestionUpdate = components['schemas']['KnowledgeGapUpdate'];
export const mentorApi = {
  updateQuestion: (id: string, body: QuestionUpdate, signal: AbortSignal) => request(`/mentor/questions/${segment(id)}`, questionSchema, { method: 'PATCH', body, signal }),
  upload: (body: FormData, signal: AbortSignal) => request('/mentor/documents', mentorDocumentSchema, { method: 'POST', body, signal }),
  retryDocument: (id: string, signal: AbortSignal) => request(`/mentor/documents/${segment(id)}/retry`, mentorDocumentSchema, { method: 'POST', signal }),
  questions: (status: string, signal: AbortSignal) => request('/mentor/questions', z.object({ items: z.array(questionSchema) }), { signal, search: { status: status || undefined } }),
  question: (id: string, signal: AbortSignal) => request(`/mentor/questions/${segment(id)}`, questionSchema, { signal }),
  documents: (signal: AbortSignal) => request('/mentor/documents', z.object({ items: z.array(mentorDocumentSchema) }), { signal }),
  document: (id: string, signal: AbortSignal) => request(`/mentor/documents/${segment(id)}`, mentorDocumentDetailSchema, { signal }),
  interns: (signal: AbortSignal) => request('/mentor/interns', z.object({ items: z.array(internSchema) }), { signal }),
  plan: (id: string, signal: AbortSignal) => request(`/mentor/interns/${segment(id)}/plan`, planSchema, { signal }),
};
