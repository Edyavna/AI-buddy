import { useEffect, useRef, useState } from 'react';
import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { InlineError, Spinner } from '@/shared/ui';
import { accessRequestsApi } from './api';
import { accessRequestSchema } from './schemas';
import type { AccessRequest } from './schemas';
import { AccessRequestCard } from './AccessRequestCard';
import { confirmAccessRequestShared } from './confirm';
import styles from './AccessRequestPanel.module.css';

const POLL_INTERVAL_MS = 5000;
const accessDenied = (error: unknown) => error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);
const ACTIVE_STATUSES: ReadonlyArray<AccessRequest['status']> = ['submitting', 'pending'];
const RETRYABLE_STATUSES: ReadonlyArray<AccessRequest['status']> = ['draft', 'failed'];

export interface AccessRequestPanelProps {
  requestId: string;
  onDiscuss?: () => void;
}

function confirmErrorMessage(error: unknown): string | null {
  if (accessDenied(error) || !(error instanceof Error)) return null;
  if (error instanceof ClientError) {
    if (error.code === 'DRAFT_CHANGED') {
      // Сервер отклонил подтверждение устаревшей версии: GET уже подтянул свежий черновик,
      // нужно только явное новое нажатие (behavior.md: 409 DRAFT_CHANGED).
      return 'Параметры заявки изменились. Проверьте новую версию и подтвердите отправку.';
    }
  }
  return error.message;
}

// Единый контейнер карточки заявки (integration.md §4): общий кеш, GET со всеми
// состояниями, confirm с одной активной мутацией на requestId, polling 5 с.
export function AccessRequestPanel({ requestId, onDiscuss }: AccessRequestPanelProps) {
  const queryClient = useQueryClient();
  const [isInView, setIsInView] = useState(true);
  const [rootNode, setRootNode] = useState<HTMLDivElement | null>(null);
  const mutationKey = ['access-requests', 'confirm', requestId] as const;
  const isConfirming = useIsMutating({ mutationKey, exact: true }) > 0;
  const inViewRef = useRef(true);
  const refetchRef = useRef<() => Promise<unknown>>(async () => {});
  const queryKey = queryKeys.request(requestId);

  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }): Promise<AccessRequest> => {
      try {
        const { data } = await accessRequestsApi.getAccessRequest(requestId, signal);
        if (!data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
        const parsed = accessRequestSchema.parse(data);
        // Устаревший GET (начался до более свежего confirm/события) не откатывает карточку.
        const cached = queryClient.getQueryData<AccessRequest | null>(queryKey);
        if (cached && parsed.revision < cached.revision) return cached;
        return parsed;
      } catch (error) {
        if (accessDenied(error)) queryClient.setQueryData<AccessRequest | null>(queryKey, null);
        throw error;
      }
    },
    refetchOnWindowFocus: false,
    // submitting/pending — опрос раз в 5 с. Пока вкладка не активна, тики интервала
    // не выполняют запрос (focusManager в react-query); не видна карточка — интервал выключен.
    refetchInterval: (q) => {
      const status = q.state.data?.status;
      if (!status || !ACTIVE_STATUSES.includes(status) || !isInView) return false;
      return POLL_INTERVAL_MS;
    },
  });
  refetchRef.current = () => query.refetch();

  // Карточка вне вьюпорта не опрашивает сервер.
  useEffect(() => {
    if (!rootNode || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(([entry]) => {
      const visible = entry?.isIntersecting ?? true;
      inViewRef.current = visible;
      setIsInView(visible);
    });
    observer.observe(rootNode);
    return () => observer.disconnect();
  }, [rootNode]);

  // Возвращение на вкладку: сразу сверить видимую активную заявку, не дожидаясь интервала.
  useEffect(() => {
    const onVisibilityChange = () => {
      if (document.visibilityState !== 'visible' || !inViewRef.current) return;
      const status = queryClient.getQueryData<AccessRequest | null>(queryKeys.request(requestId))?.status;
      if (status && ACTIVE_STATUSES.includes(status)) void refetchRef.current();
    };
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => document.removeEventListener('visibilitychange', onVisibilityChange);
  }, [queryClient, requestId]);

  // A refetch error does not discard cached data. Access denial must hide the
  // resource, reason and actions; temporary network/5xx errors keep last state.
  const unavailable = accessDenied(query.error) || query.data === null;
  const request = unavailable ? undefined : query.data;

  // Смена статуса (polling/confirm): подтянуть список заявок и план (integration.md §3).
  const prevStatusRef = useRef(request?.status);
  useEffect(() => {
    if (!request || prevStatusRef.current === request.status) return;
    prevStatusRef.current = request.status;
    void queryClient.invalidateQueries({ queryKey: queryKeys.requests() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.plan() });
  }, [request, queryClient]);

  const confirm = useMutation({
    mutationKey,
    // Отображённая версия черновика — единственное, что отправляется на сервер.
    mutationFn: (draftVersion: number) =>
      confirmAccessRequestShared(queryClient, requestId, draftVersion),
    onError: error => {
      if (!accessDenied(error)) return;
      queryClient.setQueryData<AccessRequest | null>(queryKey, null);
      void query.refetch();
    },
  });

  if (query.isPending) {
    return (
      <div className={styles.state} role="status" aria-label="Загрузка заявки">
        <Spinner size="sm" />
        <span className={styles.stateText}>Загружаем заявку…</span>
      </div>
    );
  }

  if (!request) {
    const notFound = query.isError && query.error instanceof ClientError && query.error.status === 404;
    return (
      <div className={styles.state}>
        {notFound ? (
          <span className={styles.stateText}>Заявка недоступна или удалена</span>
        ) : (
          <InlineError
            message={query.error instanceof Error ? query.error.message : 'Не удалось загрузить заявку'}
            onRetry={() => { confirm.reset(); void query.refetch(); }}
            isRetrying={query.isRefetching}
          />
        )}
      </div>
    );
  }

  const confirmError =
    confirm.error && RETRYABLE_STATUSES.includes(request.status)
      ? confirmErrorMessage(confirm.error)
      : null;

  return (
    <div ref={setRootNode}>
      <AccessRequestCard
        request={request}
        isConfirming={isConfirming}
        {...(confirmError ? { error: confirmError } : {})}
        refreshFailed={query.isError}
        onConfirm={() => confirm.mutate(request.draft_version)}
        {...(onDiscuss ? { onDiscuss } : {})}
      />
    </div>
  );
}
