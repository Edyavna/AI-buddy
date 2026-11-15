import { hashKey, type QueryClient, type QueryKey } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { queryRequestSignal, revokeQueryAccess } from '@/shared/api/query-access';

export const chatScope = ['chat'] as const;
export const chatAccessDenied = (error: unknown) =>
  error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);
export const chatSignal = (client: QueryClient, signal?: AbortSignal) =>
  queryRequestSignal(client, chatScope, signal);

export function clearChatAccess(client: QueryClient, currentRead?: QueryKey) {
  revokeQueryAccess(client, chatScope);
  for (const query of client.getQueryCache().findAll({ queryKey: chatScope })) {
    if (!currentRead || hashKey(query.queryKey) !== hashKey(currentRead)) {
      revokeQueryAccess(client, query.queryKey);
      void client.cancelQueries({ queryKey: query.queryKey, exact: true });
    }
    client.setQueryData(query.queryKey, null);
  }
}
