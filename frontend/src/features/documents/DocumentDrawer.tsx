import { useRef, type KeyboardEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { apiRequest, ClientError, queryKeys } from '@/shared/api';
import { queryRequestSignal, revokeQueryAccess } from '@/shared/api/query-access';
import { Drawer, InlineError, Spinner } from '@/shared/ui';
import { MarkdownContent } from './MarkdownContent';
import styles from './DocumentDrawer.module.css';

const documentSchema = z.object({ id: z.string(), title: z.string(), content: z.string(), updated_at: z.iso.datetime() });
export type DocumentDrawerProps = { documentId: string | null; onClose: () => void };

export function DocumentDrawer({ documentId, onClose }: DocumentDrawerProps) {
  const root = useRef<HTMLDivElement>(null);
  const client = useQueryClient();
  const key = queryKeys.document(documentId ?? '');
  const query = useQuery({
    queryKey: key, enabled: documentId !== null,
    queryFn: async ({ signal }) => {
      const scope = queryRequestSignal(client, key, signal);
      try {
        const result = await apiRequest(`/documents/${encodeURIComponent(documentId!)}`, documentSchema, { signal: scope });
        scope.throwIfAborted();
        if (!result.data) throw new ClientError('protocol', 'Документ не получен');
        return result.data;
      } catch (error) {
        scope.throwIfAborted();
        if (error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0)) {
          revokeQueryAccess(client, key);
          client.setQueryData(key, null);
        }
        throw error;
      }
    },
  });
  // Keep the shared Drawer; complete keyboard containment locally for this modal.
  function trapFocus(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Tab') return;
    const controls = root.current?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], [tabindex="0"]');
    const first = controls?.[0], last = controls?.[controls.length - 1];
    if (!first || !last) return;
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }
  const notFound = query.error instanceof ClientError && query.error.status === 404;
  const unavailable = query.isError && query.error instanceof ClientError &&
    [401, 403, 404].includes(query.error.status ?? 0);
  // A failed refetch retains query.data. Access errors must hide both the body
  // and the title, including the dialog's accessible name.
  const visibleDocument = unavailable ? undefined : query.data;
  return <div ref={root} onKeyDown={trapFocus} className={styles.host}>
    <Drawer isOpen={documentId !== null} onClose={onClose} title={visibleDocument?.title ?? 'Источник ответа'} width="lg" className={styles.documentDrawer ?? ''}>
      {query.isPending && <div role="status"><Spinner /><p>Загружаем документ…</p></div>}
      {query.isError && <InlineError message={notFound ? 'Документ недоступен или удалён' : query.error.message}
        {...(!notFound && { onRetry: () => void query.refetch(), isRetrying: query.isRefetching })} />}
      {visibleDocument && <MarkdownContent content={visibleDocument.content} />}
    </Drawer>
  </div>;
}
