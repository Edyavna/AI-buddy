import { useQuery } from '@tanstack/react-query';
import { ClientError, queryKeys } from '@/shared/api';
import { InlineError, Spinner } from '@/shared/ui';
import { accessRequestsApi } from './api';
import { accessRequestSchema } from './schemas';
import type { AccessRequest } from './schemas';
import { AccessRequestCard } from './AccessRequestCard';
import styles from './AccessRequestPanel.module.css';

export interface AccessRequestPanelProps {
  requestId: string;
  onDiscuss?: () => void;
}

// The single shared container for an access request (integration.md §4):
// own query against the common cache, loading / not-found / error states.
// Confirm and polling arrive with the request flow (#5).
export function AccessRequestPanel({ requestId, onDiscuss }: AccessRequestPanelProps) {
  const query = useQuery({
    queryKey: queryKeys.request(requestId),
    queryFn: async ({ signal }): Promise<AccessRequest> => {
      const { data } = await accessRequestsApi.getAccessRequest(requestId, signal);
      if (!data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
      const parsed = accessRequestSchema.safeParse(data);
      if (!parsed.success) throw new ClientError('protocol', 'Ответ сервера не соответствует контракту');
      return parsed.data;
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

  if (query.isError) {
    const notFound = query.error instanceof ClientError && query.error.status === 404;
    return (
      <div className={styles.state}>
        {notFound ? (
          <span className={styles.stateText}>Заявка недоступна или удалена</span>
        ) : (
          <InlineError
            message={query.error instanceof Error ? query.error.message : 'Не удалось загрузить заявку'}
            onRetry={() => void query.refetch()}
            isRetrying={query.isRefetching}
          />
        )}
      </div>
    );
  }

  return <AccessRequestCard request={query.data} {...(onDiscuss ? { onDiscuss } : {})} />;
}
