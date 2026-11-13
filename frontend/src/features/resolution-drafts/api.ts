import { z } from 'zod';
import { apiRequest, ClientError, type RequestOptions } from '@/shared/api';
import type { components } from '@/shared/api/generated/schema';

const source = z.object({ id: z.string(), direction: z.enum(['mentor', 'hr']), team_id: z.string(), question: z.string(), resolution: z.string(), step_id: z.string().nullable(), revision: z.number().int().positive() });
const target = z.object({ id: z.string(), title: z.string(), version: z.number().int().positive(), content: z.string(), team_ids: z.array(z.string()) });
export const resolutionDraftSchema = z.object({
  revision: z.number().int().positive(), title: z.string(), content: z.string(), target_document_id: z.string().nullable(), base_version: z.number().int().positive().nullable(),
  source, team_ids: z.array(z.string()), base_content: z.string(), document_id: z.string().nullable(), document_version: z.number().int().positive().nullable(), status: z.enum(['draft', 'indexing', 'ready', 'failed']), error: z.string().nullable(),
});
export const workspaceSchema = z.object({ source, draft: resolutionDraftSchema.nullable(), targets: z.array(target) });
export type Direction = 'mentor' | 'hr';
export type Draft = z.infer<typeof resolutionDraftSchema>;
export type Workspace = z.infer<typeof workspaceSchema>;
export type SaveDraft = components['schemas']['SaveResolutionDraft'];
const root = (direction: Direction, id: string) => `/${direction}/questions/${encodeURIComponent(id)}/instruction-draft`;
export const draftKey = (direction: Direction, id: string) => ['resolution-drafts', direction, id] as const;
async function request<T>(path: string, schema: z.ZodType<T>, options: RequestOptions) {
  const result = await apiRequest(path, schema, options);
  options.signal?.throwIfAborted();
  if (!result.data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
  return result.data;
}
export const resolutionApi = {
  workspace: (direction: Direction, id: string, signal: AbortSignal) => request(root(direction, id), workspaceSchema, { signal }),
  target: (direction: Direction, id: string, targetId: string, signal: AbortSignal) => request(`${root(direction, id)}/targets/${encodeURIComponent(targetId)}`, target, { signal }),
  save: (direction: Direction, id: string, body: SaveDraft, signal: AbortSignal) => request(root(direction, id), resolutionDraftSchema, { method: 'PUT', body, signal }),
  generate: (direction: Direction, id: string, signal: AbortSignal) => request(`${root(direction, id)}/generate`, z.object({ title: z.string(), content: z.string(), mode: z.enum(['model', 'demo']) }), { method: 'POST', signal }),
  publish: (direction: Direction, id: string, revision: number, retry: boolean, signal: AbortSignal) => request(`${root(direction, id)}/${retry ? 'retry' : 'publish'}`, resolutionDraftSchema, { method: 'POST', body: { revision, reviewed: true }, signal }),
};
