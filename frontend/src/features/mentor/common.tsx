import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { InlineError, Spinner } from '@/shared/ui';

export const accessDenied = (error: unknown) => error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);
export function useMentorQuery<T>(key: QueryKey, load: (signal: AbortSignal) => Promise<T>) {
  const queryClient = useQueryClient();
  const query = useQuery<T | null>({
    queryKey: key,
    queryFn: async ({ signal }) => {
      try { return await load(signal); }
      catch (error) {
        // Remove revoked content from the shared cache. The null marker survives
        // transient errors and remounts; only a successful response replaces it.
        if (!signal.aborted && accessDenied(error)) queryClient.setQueryData<T | null>(key, null);
        throw error;
      }
    },
    retry: false,
    refetchInterval: q => q.state.data === null || accessDenied(q.state.error) ? false : 5000,
    refetchIntervalInBackground: false,
  });
  return { ...query, data: accessDenied(query.error) ? undefined : query.data ?? undefined };
}
export function QueryFeedback({ query }: { query: { isPending: boolean; isError: boolean; error: Error | null; isFetching: boolean; refetch: () => unknown } }) {
  return <>{query.isPending && <div role="status"><Spinner /> Загружаем…</div>}
    {query.isError && <InlineError message={accessDenied(query.error) ? 'Данные недоступны или удалены' : query.error?.message ?? 'Не удалось обновить данные'} onRetry={() => void query.refetch()} isRetrying={query.isFetching} />}</>;
}
// Mutations are screen-owned: leaving the screen/account cancels transport and callbacks.
export function useAction() {
  const controller = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  useEffect(() => () => { controller.current?.abort(); }, []);
  async function run(work: (signal: AbortSignal) => Promise<void>) {
    if (controller.current) return;
    const active = new AbortController(); controller.current = active; setBusy(true); setError('');
    try { await work(active.signal); }
    catch (e) { if (!active.signal.aborted) setError(e instanceof Error ? e.message : 'Не удалось выполнить действие'); }
    finally { if (!active.signal.aborted) { controller.current = null; setBusy(false); } }
  }
  return { run, busy, error, setError };
}
export const questionLabels = { new: 'Новый', in_progress: 'В работе', resolved: 'Решён' };
export const documentLabels = { processing: 'Индексируется', ready: 'Готов к поиску', failed: 'Ошибка индексации' };
export const dateLabel = (value: string) => new Date(value).toLocaleString('ru-RU');
