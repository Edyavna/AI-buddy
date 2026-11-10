import type { QueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { accessRequestsApi } from './api';
import { accessRequestSchema } from './schemas';
import type { AccessRequest } from './schemas';
import { acceptAccessRequest } from './cache';

// Одна активная мутация confirm на requestId во всём приложении (integration.md §4):
// все копии карточки (чат + план) делят один in-flight promise, поэтому двойное нажатие
// или два смонтированных экземпляра никогда не порождают второй сетевой запрос.
// Повтор идёт по тому же requestId и не создаёт новую заявку (behavior.md §4).
const inflight = new Map<string, Promise<AccessRequest>>();

export function confirmAccessRequestShared(
  queryClient: QueryClient,
  requestId: string,
  draftVersion: number,
): Promise<AccessRequest> {
  const pending = inflight.get(requestId);
  if (pending) return pending;

  const task = (async (): Promise<AccessRequest> => {
    const { status, data } = await accessRequestsApi.confirmAccessRequest(requestId, draftVersion);
    if (!data) throw new ClientError('protocol', 'Сервер вернул пустой ответ', { status });
    const request = accessRequestSchema.parse(data);

    const accepted = acceptAccessRequest(queryClient, request);
    if (accepted === 'conflict') {
      // Равная revision с другим содержимым: кеш противоречит ответу — запросить факт.
      await queryClient.invalidateQueries({ queryKey: queryKeys.request(requestId) });
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.requests() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
    return request;
  })().finally(() => {
    inflight.delete(requestId);
  });

  inflight.set(requestId, task);
  return task;
}
