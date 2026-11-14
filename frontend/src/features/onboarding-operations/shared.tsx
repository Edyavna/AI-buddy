import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import styles from './operations.module.css';

export function useWorkspace<T>(path: string, schema: z.ZodType<T>, search?: Record<string, string | undefined>) {
  const [state, setState] = useState<{ data?: T | undefined; error?: string; loading: boolean }>({ loading: true });
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState('');
  const request = useRef<AbortController | undefined>(undefined);
  const mutation = useRef<AbortController | undefined>(undefined);
  const encodedSearch = JSON.stringify(search ?? {});
  const load = useCallback(async (refresh = false) => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    if (!refresh) setState({ loading: true });
    try {
      const response = await apiRequest(path, schema, { signal: controller.signal, search: JSON.parse(encodedSearch) });
      if (!controller.signal.aborted) setState({ data: response.data, loading: false });
    } catch (error) {
      if (!controller.signal.aborted) setState({ error: errorText(error), loading: false });
    }
  }, [path, schema, encodedSearch]);
  useEffect(() => {
    setPending(false);
    setMessage('');
    void load();
    return () => {
      request.current?.abort();
      mutation.current?.abort();
      mutation.current = undefined;
    };
  }, [load]);
  async function send(body: unknown) {
    if (mutation.current) return false;
    const controller = new AbortController();
    mutation.current = controller;
    setPending(true);
    setMessage('');
    let accepted = false;
    try {
      await apiRequest(path, z.object({ revision: z.number() }), { method: 'POST', body, signal: controller.signal });
      accepted = true;
    } catch (error) {
      if (!controller.signal.aborted) setMessage(errorText(error));
    } finally {
      if (!controller.signal.aborted) {
        await load(true);
        if (!controller.signal.aborted) {
          setPending(false);
          mutation.current = undefined;
        }
      }
    }
    return accepted && !controller.signal.aborted;
  }
  return { ...state, pending, message, load, send };
}
function errorText(error: unknown) {
  return error instanceof ClientError ? error.message : 'Операцию не удалось выполнить.';
}
export function LoadState({ loading, error, retry, children }: { loading: boolean; error: string | undefined; retry: () => void; children: ReactNode }) {
  if (loading) return <p role="status">Загрузка…</p>;
  if (error) return <div role="alert"><p>{error}</p><button onClick={retry}>Повторить</button></div>;
  return <>{children}</>;
}
export function localDate(value: string) {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
export function isoDate(value: string) { return value ? new Date(value).toISOString() : null; }
export { styles };
