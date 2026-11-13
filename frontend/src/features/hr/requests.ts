import { useRef, useState } from 'react';
import { useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { queryRequestSignal, revokeQueryAccess } from '@/shared/api/query-access';

export function usePrivateQuery<T>(key: QueryKey, fetch: (signal: AbortSignal) => Promise<T>, enabled = true, refresh?: number | false) {
  const client = useQueryClient();
  return useQuery<T | null>({
    queryKey: key, enabled, retry: false, refetchInterval: refresh ?? false,
    queryFn: async ({ signal }) => {
      const scope = queryRequestSignal(client, key, signal);
      try {
        const result = await fetch(scope);
        scope.throwIfAborted();
        return result;
      } catch (error) {
        scope.throwIfAborted();
        if (denied(error)) {
          revokeQueryAccess(client, key);
          client.setQueryData(key, null);
        }
        throw error;
      }
    },
  });
}

export function denied(error: unknown) {
  return error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);
}

function uncertain(error: unknown) {
  return error instanceof ClientError && (['network', 'protocol', 'aborted'].includes(error.kind) || (error.status ?? 0) >= 500);
}

export function useOperation(key: QueryKey) {
  const client = useQueryClient();
  const previous = useRef<{ payload: string; id: string } | null>(null);
  const active = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  async function run<T>(
    payload: unknown, send: (id: string, signal: AbortSignal) => Promise<T>,
    reconcile?: (signal: AbortSignal) => Promise<T | undefined>,
  ): Promise<T | undefined> {
    if (active.current) return;
    active.current = true;
    setPending(true);
    setError('');
    const stamp = JSON.stringify(payload);
    if (previous.current?.payload !== stamp) previous.current = { payload: stamp, id: crypto.randomUUID() };
    const signal = queryRequestSignal(client, key);
    try {
      let result: T | undefined;
      try {
        result = await send(previous.current.id, AbortSignal.any([signal, AbortSignal.timeout(10000)]));
      } catch (error) {
        signal.throwIfAborted();
        if (!reconcile || !uncertain(error)) throw error;
        result = await reconcile(AbortSignal.any([signal, AbortSignal.timeout(10000)]));
        if (result === undefined) throw error;
      }
      signal.throwIfAborted();
      previous.current = null;
      return result;
    } catch (error) {
      if (!signal.aborted) {
        if (denied(error)) {
          revokeQueryAccess(client, key);
          await client.cancelQueries({ queryKey: key }, { revert: false });
          client.setQueryData(key, null);
        }
        setError(error instanceof Error ? error.message : 'Операцию не удалось подтвердить. Повторите попытку.');
      }
    } finally {
      active.current = false;
      setPending(false);
    }
  }
  return { run, pending, error };
}
