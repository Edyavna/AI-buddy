import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import { abortSessionRequests } from '@/shared/api/session-signal';

const scopeKey = ['session', 'access-scope'] as const;
const scopeSchema = z.object({
  version: z.string(),
  teams: z.array(z.object({ id: z.string(), name: z.string() })),
});

export function useAccessScope(userId: string | undefined) {
  return useQuery({
    queryKey: [...scopeKey, userId],
    enabled: Boolean(userId),
    retry: false,
    refetchInterval: 5000,
    refetchOnWindowFocus: 'always',
    queryFn: async ({ signal }) => {
      const result = await apiRequest('/me/access-scope', scopeSchema, { signal });
      if (!result.data) throw new ClientError('protocol', 'Сервер вернул пустую область доступа');
      return result.data;
    },
  });
}

export function useAccessScopeRefresh(userId: string | undefined) {
  const query = useAccessScope(userId);
  const client = useQueryClient();
  const previous = useRef<{ userId?: string; version?: string }>(null);
  const denied = query.error instanceof ClientError && [401, 403, 404].includes(query.error.status ?? 0);
  const version = denied ? 'denied' : query.data?.version;
  const [generation, setGeneration] = useState(0);
  useEffect(() => {
    if (!userId || !version) return;
    const old = previous.current;
    previous.current = { userId, version };
    if (!old || old.userId !== userId || old.version === version) return;
    abortSessionRequests(client);
    const protectedQueries = { predicate: ({ queryKey }: { queryKey: readonly unknown[] }) =>
      queryKey[0] !== 'session' && queryKey[0] !== 'me' };
    void client.cancelQueries(protectedQueries, { revert: false });
    void client.resetQueries(protectedQueries);
    setGeneration(n => n + 1);
  }, [client, userId, version]);
  return denied ? 'denied' : generation;
}
