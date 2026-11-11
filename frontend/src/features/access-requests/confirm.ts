import type { QueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { sessionSignal } from '@/shared/api/session-signal';
import { accessRequestsApi } from './api';
import type { AccessRequest } from './schemas';
import { acceptAccessRequest } from './cache';

const CONFIRM_TIMEOUT_MS = 10_000;
const inflight = new WeakMap<AbortSignal, Map<string, Promise<AccessRequest>>>();

export function confirmAccessRequestShared(
  queryClient: QueryClient,
  requestId: string,
  draftVersion: number,
): Promise<AccessRequest> {
  const session = sessionSignal(queryClient);
  let operations = inflight.get(session);
  if (!operations) {
    operations = new Map();
    inflight.set(session, operations);
  }
  const pending = operations.get(requestId);
  if (pending) return pending;

  const key = queryKeys.request(requestId);
  const task = (async () => {
    const reconcile = async () => {
      const signal = AbortSignal.any([session, AbortSignal.timeout(CONFIRM_TIMEOUT_MS)]);
      const { data } = await accessRequestsApi.getAccessRequest(requestId, signal);
      if (!data || data.id !== requestId) throw new ClientError('protocol', 'Сервер вернул некорректную заявку');
      session.throwIfAborted();
      const cached = queryClient.getQueryData<AccessRequest>(key);
      if (cached && cached.revision > data.revision) return cached;
      queryClient.setQueryData(key, data);
      return data;
    };

    try {
      const timeout = AbortSignal.timeout(CONFIRM_TIMEOUT_MS);
      const before = queryClient.getQueryData<AccessRequest>(key);
      let request: AccessRequest;
      try {
        const { data, status } = await accessRequestsApi.confirmAccessRequest(
          requestId, draftVersion, AbortSignal.any([session, timeout]),
        );
        if (!data || data.id !== requestId) throw new ClientError('protocol', 'Сервер вернул некорректную заявку', { status });
        request = data;
      } catch (error) {
        session.throwIfAborted();
        const changed = error instanceof ClientError && error.code === 'DRAFT_CHANGED';
        const uncertain = timeout.aborted || error instanceof ClientError &&
          (error.kind === 'network' || error.kind === 'protocol' || (error.status ?? 0) >= 500);
        if (!changed && !uncertain) throw error;
        request = await reconcile();
        const retryUnchanged = request.status === 'failed' && (!before || request.revision <= before.revision);
        if (request.status !== 'draft' && !retryUnchanged) return request;
        if (changed || request.draft_version !== draftVersion) {
          throw new ClientError('http', 'Черновик изменён', { code: 'DRAFT_CHANGED', status: 409 });
        }
        throw new ClientError('network', 'Исход отправки неизвестен. Заявка доступна для повторного подтверждения.');
      }
      session.throwIfAborted();
      if (acceptAccessRequest(queryClient, request) === 'conflict') return reconcile();
      return request;
    } finally {
      if (!session.aborted) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.requests() });
        void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
      }
    }
  })().finally(() => {
    operations.delete(requestId);
  });
  operations.set(requestId, task);
  return task;
}
