import { hashKey, useQueryClient, type QueryClient, type QueryKey } from '@tanstack/react-query';
import { accessDenied, useMentorQuery } from '@/features/mentor/common';
import { queryRequestSignal, revokeQueryAccess } from '@/shared/api/query-access';
import { queueKey } from './api';

const accessKey = ['action-queue-access'] as const;
const invitationPrefix = ['checkpoint-invitation'] as const;

export const actionQueueSignal = (client: QueryClient, signal: AbortSignal) =>
  queryRequestSignal(client, accessKey, signal);

// A denial invalidates the whole queue scope, including other filters and writes.
// Keep the failing read alive so React Query still exposes its actual error.
export function clearActionQueueAccess(client: QueryClient, currentRead?: QueryKey) {
  revokeQueryAccess(client, accessKey);
  for (const prefix of [queueKey, invitationPrefix]) {
    for (const query of client.getQueryCache().findAll({ queryKey: prefix })) {
      if (!currentRead || hashKey(query.queryKey) !== hashKey(currentRead)) {
        revokeQueryAccess(client, query.queryKey);
        void client.cancelQueries({ queryKey: query.queryKey, exact: true });
      }
      client.setQueryData(query.queryKey, null);
    }
  }
}

export function useActionQueueQuery<T>(key: QueryKey, load: (signal: AbortSignal) => Promise<T>, gcTime?: number) {
  const client = useQueryClient();
  return useMentorQuery(key, async owner => {
    const signal = actionQueueSignal(client, owner);
    try {
      const data = await load(signal);
      signal.throwIfAborted();
      return data;
    } catch (error) {
      if (!signal.aborted && accessDenied(error)) clearActionQueueAccess(client, key);
      throw error;
    }
  }, gcTime);
}
