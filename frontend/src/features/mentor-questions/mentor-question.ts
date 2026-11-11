import { queryOptions, type QueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import type { components } from '@/shared/api/generated/schema';
import { sessionSignal } from '@/shared/api/session-signal';

type Question = components['schemas']['InternKnowledgeGap'];
const schema: z.ZodType<Question> = z.object({
  id: z.uuid(), message_id: z.string(), status: z.enum(['new', 'in_progress', 'resolved']),
  revision: z.number().int().positive(), resolution: z.string().nullable(),
  document_ids: z.array(z.string()), created_at: z.iso.datetime(), updated_at: z.iso.datetime(),
});
const lookupSchema = z.object({ question: schema.nullable() });
const pending = new WeakMap<AbortSignal, Map<string, Promise<Question | null>>>();
const path = (id: string) => `/messages/${encodeURIComponent(id)}/knowledge-gap`;
const key = (id: string) => ['chat', 'knowledge-gap', id] as const;
export const accessDenied = (error: unknown) => error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);

export function getQuestionOptions(client: QueryClient, messageId: string) {
  return queryOptions({
    queryKey: key(messageId),
    queryFn: async ({ signal }) => {
      const { data } = await apiRequest(path(messageId), lookupSchema, {
        signal: AbortSignal.any([signal, sessionSignal(client), AbortSignal.timeout(10_000)]),
      });
      if (!data || (data.question && data.question.message_id !== messageId)) {
        throw new ClientError('protocol', 'Сервер вернул некорректный статус передачи');
      }
      const old = client.getQueryData<Question | null>(key(messageId));
      return old && (!data.question || old.revision > data.question.revision) ? old : data.question;
    },
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: 'always',
  });
}

export function sendQuestion(client: QueryClient, messageId: string) {
  const session = sessionSignal(client);
  let operations = pending.get(session);
  if (!operations) {
    operations = new Map();
    pending.set(session, operations);
  }
  const existing = operations.get(messageId);
  if (existing) return existing;

  const lookup = getQuestionOptions(client, messageId);
  const work = (async () => {
    const saved = await client.fetchQuery(lookup);
    if (saved) return saved;
    session.throwIfAborted();

    const timeout = AbortSignal.timeout(10_000);
    let result: Question;
    try {
      const { data } = await apiRequest(path(messageId), schema, {
        method: 'POST', signal: AbortSignal.any([session, timeout]),
      });
      if (!data || data.message_id !== messageId) {
        throw new ClientError('protocol', 'Сервер вернул некорректную передачу');
      }
      result = data;
    } catch (error) {
      session.throwIfAborted();
      const uncertain = timeout.aborted || error instanceof ClientError &&
        (error.kind === 'network' || error.kind === 'protocol' || (error.status ?? 0) >= 500);
      if (!uncertain) {
        if (accessDenied(error)) void client.fetchQuery(lookup).catch(() => {});
        throw error;
      }
      // Cancel a GET begun before the POST, then reconcile against persisted state.
      await client.cancelQueries({ queryKey: key(messageId), exact: true });
      session.throwIfAborted();
      const recovered = await client.fetchQuery(lookup);
      if (recovered) return recovered;
      throw new Error('Передача не подтверждена. Проверьте соединение и повторите попытку.');
    }
    session.throwIfAborted();
    client.setQueryData<Question | null>(key(messageId), old => old && old.revision > result.revision ? old : result);
    return result;
  })().finally(() => operations.delete(messageId));
  operations.set(messageId, work);
  return work;
}
