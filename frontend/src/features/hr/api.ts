import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import type { RequestOptions } from '@/shared/api/client';
import { contextInputSchema, contextSchema } from '@/features/question-context/api';

export const directionSchema = z.enum(['mentor', 'hr']);
export const questionStatusSchema = z.enum(['new', 'in_progress', 'resolved']);
export const routingSchema = z.object({
  direction: z.enum(['mentor', 'hr', 'needs_clarification']),
  reason: z.string(),
  clarification: z.string().nullable(),
  split: z.boolean(),
});
const draftSchema = z.object({ direction: directionSchema, question: z.string(), context_excerpt: z.string(),context_snapshot:contextInputSchema.optional() });
const questionSchema = draftSchema.extend({
  context_snapshot:contextSchema.optional(),
  id: z.string(), message_id: z.string(), team_id: z.string(),
  original_direction: directionSchema,
  student: z.object({ id: z.string(), name: z.string() }),
  specialist_id: z.string().nullable(), step_id: z.string().nullable(),
  status: questionStatusSchema, revision: z.number().int().positive(),
  resolution: z.string().nullable(), document_ids: z.array(z.string()),
  routing: routingSchema.nullable(), applied_revision: z.number().int().positive().nullable(),
  created_at: z.string(), updated_at: z.string(),
});
const messageSchema = z.object({
  items: z.array(questionSchema), drafts: z.array(draftSchema), question: z.string(),
  available_directions: z.array(directionSchema), routing: routingSchema.nullable(),
});
const historySchema = z.object({
  question: questionSchema, history: z.array(z.object({ revision: z.number(), snapshot: z.record(z.string(), z.unknown()) })),
});
const documentSchema = z.object({
  id: z.string(), author_id: z.string(), direction: z.literal('hr'),
  title: z.string(), content: z.string(), team_ids: z.array(z.string()),
  version: z.number().int().positive(), published_version: z.number().int().positive().nullable(),
  status: z.enum(['draft', 'indexing', 'ready', 'failed']), revoked: z.boolean(),
  error: z.string().nullable(), updated_at: z.string(),
});

export type Direction = z.infer<typeof directionSchema>;
export type RoutedQuestion = z.infer<typeof questionSchema>;
export type Draft = z.infer<typeof draftSchema>;
export type HRDocument = z.infer<typeof documentSchema>;
export type QuestionAction = 'claim' | 'answer' | 'resolve' | 'reopen';
type DocumentFields = Pick<HRDocument, 'title' | 'content' | 'team_ids'>;
const part = encodeURIComponent;
const messagePath = (id: string) => `/messages/${part(id)}/questions`;
const questionPath = (id: string) => `/hr/questions/${part(id)}`;
const documentPath = (id: string) => `/hr/documents/${part(id)}`;

async function get<T>(path: string, schema: z.ZodType<T>, opts: RequestOptions): Promise<T> {
  const result = await apiRequest(path, schema, opts);
  opts.signal?.throwIfAborted();
  if (!result.data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
  return result.data;
}

export const hrApi = {
  message: (id: string, signal: AbortSignal) => get(messagePath(id), messageSchema, { signal }),
  draft: (id: string, { direction, ...body }: Draft, signal: AbortSignal) =>
    get(`/messages/${part(id)}/question-drafts/${direction}`, draftSchema, { method: 'PUT', body, signal }),
  send: (id: string, body: Draft & { operation_id: string }, signal: AbortSignal) =>
    get(messagePath(id), questionSchema, { method: 'POST', body, signal }),
  history: (id: string, signal: AbortSignal) => get(`/questions/${part(id)}`, historySchema, { signal }),
  reroute: (id: string, body: Draft & { operation_id: string; revision: number }, signal: AbortSignal) =>
    get(`/questions/${part(id)}/reroute`, questionSchema, { method: 'POST', body, signal }),
  questions: (search: Record<string, string>, signal: AbortSignal) =>
    get('/hr/questions', z.object({ items: z.array(questionSchema) }), { search, signal }),
  question: (id: string, signal: AbortSignal) => get(questionPath(id), questionSchema, { signal }),
  update: (id: string, body: { operation_id: string; revision: number; action: QuestionAction; resolution?: string; document_ids?: string[] }, signal: AbortSignal) =>
    get(questionPath(id), questionSchema, { method: 'PATCH', body, signal }),
  documents: (signal: AbortSignal) => get('/hr/documents', z.object({ items: z.array(documentSchema) }), { signal }),
  document: (id: string, signal: AbortSignal) => get(documentPath(id), documentSchema, { signal }),
  createDocument: (body: DocumentFields & { operation_id: string }, signal: AbortSignal) =>
    get('/hr/documents', documentSchema, { method: 'POST', body, signal }),
  editDocument: (id: string, body: DocumentFields & { version: number }, signal: AbortSignal) =>
    get(documentPath(id), documentSchema, { method: 'PATCH', body, signal }),
  publish: (id: string, version: number, action: 'publish' | 'retry' | 'revoke', signal: AbortSignal) =>
    get(`${documentPath(id)}/${action}`, documentSchema, { method: 'POST', body: { version }, signal }),
};

export const directionLabel: Record<Direction, string> = { mentor: 'Наставник', hr: 'HR' };
export const questionStatus: Record<RoutedQuestion['status'], string> = {
  new: 'Новое', in_progress: 'В работе', resolved: 'Решено',
};
