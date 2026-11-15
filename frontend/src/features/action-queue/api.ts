import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import type { RequestOptions } from '@/shared/api/client';

export const kinds = ['invitation', 'confirmation', 'question'] as const;
const person = z.object({ id: z.string(), name: z.string() });
const timestamp = z.iso.datetime({ offset: true });
const count = z.number().int().nonnegative();
const item = z.object({
  kind: z.enum(kinds),
  id: z.string(),
  intern: person,
  step_id: z.string().nullable(),
  step_title: z.string().nullable(),
  question_preview: z.string().nullable(),
  action: z.enum(['record_invitation', 'review_checkpoint', 'claim_question', 'answer_question']),
  since: timestamp,
  updated_at: timestamp,
});
const page = z.object({
  items: z.array(item),
  interns: z.array(person),
  counts: z.object({ invitation: count, confirmation: count, question: count }),
  total: count,
});
const invitation = z.object({
  id: z.uuid(),
  state: z.enum(['pending', 'canceled', 'completed']),
  requested_at: timestamp,
  closed_at: timestamp.nullable(),
  note: z.string().nullable(),
});
const invitationState = z.object({ invitation: invitation.nullable() });

export type QueueItem = z.infer<typeof item>;
export type Invitation = z.infer<typeof invitation>;
export type Kind = typeof kinds[number];
export const queueKey = ['action-queue'] as const;
export const invitationKey = (step: string) => ['checkpoint-invitation', step] as const;
export const pageSize = 20;

async function read<T>(path: string, schema: z.ZodType<T>, options: RequestOptions) {
  const response = await apiRequest(path, schema, options);
  if (response.data === undefined) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
  return response.data;
}

export const loadQueue = (intern: string, kind: Kind | '', offset: number, signal: AbortSignal) =>
  read('/action-queue', page, { signal, search: {
    intern_id: intern || undefined, kind: kind || undefined, offset: String(offset), limit: String(pageSize),
  } });

const internPath = (step: string) => `/onboarding/steps/${encodeURIComponent(step)}/invitation`;
const mentorPath = (intern: string, id: string) =>
  `/mentor/interns/${encodeURIComponent(intern)}/invitations/${encodeURIComponent(id)}`;
const timed = (signal: AbortSignal) => AbortSignal.any([signal, AbortSignal.timeout(10_000)]);

export const loadInvitation = async (step: string, signal: AbortSignal) =>
  (await read(internPath(step), invitationState, { signal: timed(signal) })).invitation;

async function reconcile(
  send: () => Promise<Invitation | null>,
  refresh: () => Promise<Invitation | null>,
  matches: (value: Invitation) => boolean,
  signal: AbortSignal,
) {
  let value: Invitation | null;
  try {
    value = await send();
  } catch (error) {
    signal.throwIfAborted();
    if (!(error instanceof ClientError) || (error.kind === 'http' && (error.status ?? 0) < 500)) throw error;
    value = await refresh();
  }
  signal.throwIfAborted();
  if (!value || !matches(value)) {
    throw new ClientError('protocol', 'Действие не подтверждено. Обновите данные перед повтором.');
  }
  return value;
}

export function setInvitation(step: string, id: string, action: 'request' | 'cancel', signal: AbortSignal) {
  return reconcile(
    async () => (await read(internPath(step), invitationState, { method: 'PUT', body: { id, action }, signal: timed(signal) })).invitation,
    () => loadInvitation(step, signal),
    value => value.id === id && value.state === (action === 'request' ? 'pending' : 'canceled'),
    signal,
  );
}

export function completeInvitation(intern: string, id: string, note: string, signal: AbortSignal) {
  const path = mentorPath(intern, id);
  return reconcile(
    () => read(path, invitation, { method: 'PUT', body: { note }, signal: timed(signal) }),
    () => read(path, invitation, { signal: timed(signal) }),
    value => value.id === id && value.state === 'completed',
    signal,
  );
}
