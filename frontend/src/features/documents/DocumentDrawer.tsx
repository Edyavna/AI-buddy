import { useRef, type KeyboardEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { z } from 'zod';
import { apiRequest, ClientError, queryKeys } from '@/shared/api';
import { Drawer, InlineError, Spinner } from '@/shared/ui';
import { MarkdownContent } from './MarkdownContent';
import styles from './DocumentDrawer.module.css';

const documentSchema = z.object({ id: z.string(), title: z.string(), content: z.string(), updated_at: z.iso.datetime() });
export type DocumentDrawerProps = { documentId: string | null; onClose: () => void };

export function DocumentDrawer({ documentId, onClose }: DocumentDrawerProps) {
  const root = useRef<HTMLDivElement>(null);
  const query = useQuery({
    queryKey: queryKeys.document(documentId ?? ''), enabled: documentId !== null,
    queryFn: async ({ signal }) => {
      const result = await apiRequest(`/documents/${encodeURIComponent(documentId!)}`, documentSchema, { signal });
      if (!result.data) throw new ClientError('protocol', 'Документ не получен');
      return result.data;
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
  return <div ref={root} onKeyDown={trapFocus} className={styles.host}>
    <Drawer isOpen={documentId !== null} onClose={onClose} title={query.data?.title ?? 'Источник ответа'} width="lg" className={styles.documentDrawer ?? ''}>
      {query.isPending && <div role="status"><Spinner /><p>Загружаем документ…</p></div>}
      {query.isError && <InlineError message={notFound ? 'Документ недоступен или удалён' : query.error.message}
        {...(!notFound && { onRetry: () => void query.refetch(), isRetrying: query.isRefetching })} />}
      {query.data && <MarkdownContent content={query.data.content} />}
    </Drawer>
  </div>;
}
