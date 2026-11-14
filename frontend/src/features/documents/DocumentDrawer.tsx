import { useEffect,useMemo,useRef, type KeyboardEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { apiRequest, ClientError, queryKeys } from '@/shared/api';
import { queryRequestSignal, revokeQueryAccess } from '@/shared/api/query-access';
import { Drawer, InlineError, Spinner } from '@/shared/ui';
import { MarkdownContent } from './MarkdownContent';
import { revokeDocumentContext } from './context-access';
import styles from './DocumentDrawer.module.css';

const documentSchema = z.object({ id: z.string(), title: z.string(), content: z.string(), updated_at: z.iso.datetime(),version:z.number().int().positive().optional(), original_url: z.url({ protocol: /^https$/, hostname: /^github\.com$/ }).optional() });
export type DocumentDrawerProps = { documentId: string | null; onClose: () => void; sourceMessageId?:string|undefined; contextQuestion?:{id:string;direction:'mentor'|'hr'|'intern'} };

export function DocumentDrawer({ documentId, onClose,sourceMessageId,contextQuestion }: DocumentDrawerProps) {
  const root = useRef<HTMLDivElement>(null);
  const client = useQueryClient();
  const contextId=contextQuestion?.id,contextDirection=contextQuestion?.direction;
  const contextBinding=useMemo(()=>contextId&&contextDirection?{id:contextId,direction:contextDirection}:undefined,[contextId,contextDirection]);
  const key = useMemo(()=>{const baseKey=queryKeys.document(documentId??'');return sourceMessageId?[...baseKey,'source',sourceMessageId]:contextId?[...baseKey,'question-context',contextDirection,contextId]:baseKey;},[documentId,sourceMessageId,contextId,contextDirection]);
  const query = useQuery<z.infer<typeof documentSchema>|null>({
    queryKey: key, enabled: documentId !== null,
    queryFn: async ({ signal }) => {
      const scope = queryRequestSignal(client, key, signal);
      try {
        const suffix=`context-documents/${encodeURIComponent(documentId!)}`;
        const path=sourceMessageId?`/messages/${encodeURIComponent(sourceMessageId)}/${suffix}`:contextQuestion?`${contextQuestion.direction==='intern'?'':`/${contextQuestion.direction}`}/questions/${encodeURIComponent(contextQuestion.id)}/${suffix}`:`/documents/${encodeURIComponent(documentId!)}`;
        const result = await apiRequest(path, documentSchema, { signal: scope });
        scope.throwIfAborted();
        if (!result.data) throw new ClientError('protocol', 'Документ не получен');
        return result.data;
      } catch (error) {
        scope.throwIfAborted();
        if (error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0)) {
          revokeQueryAccess(client, key);
          client.setQueryData(key, null);
          revokeDocumentContext(client,sourceMessageId,contextQuestion);
        }
        throw error;
      }
    },
  });
  const loadedVersion=query.data?.version;
  const loadedId=query.data?.id;
  useEffect(()=>{
    if(!sourceMessageId||!documentId||loadedId!==documentId||!loadedVersion||!query.isSuccess||query.isFetching)return;
    const active=new AbortController();
    const scope=AbortSignal.any([queryRequestSignal(client,key,active.signal),AbortSignal.timeout(10000)]);
    void apiRequest(`/messages/${encodeURIComponent(sourceMessageId)}/context-documents/${encodeURIComponent(documentId)}/opened`,z.object({opened:z.literal(true)}),{method:'POST',body:{version:loadedVersion},signal:scope}).then(()=>{
      scope.throwIfAborted();return client.invalidateQueries({queryKey:['question-context',sourceMessageId]});
    }).catch((error:unknown)=>{
      if(!scope.aborted&&error instanceof ClientError&&[401,403,404].includes(error.status??0)){revokeQueryAccess(client,key);client.setQueryData(key,null);revokeDocumentContext(client,sourceMessageId,contextBinding);}
    });
    return()=>active.abort();
  },[client,key,documentId,sourceMessageId,loadedId,loadedVersion,query.isSuccess,query.isFetching,contextBinding]);
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
      {query.data===null&&!query.isError&&<InlineError message="Документ недоступен или доступ изменился" onRetry={()=>void query.refetch()}/>}
      {visibleDocument?.original_url && <a href={visibleDocument.original_url} target="_blank" rel="noopener noreferrer">Открыть в GitHub</a>}
      {visibleDocument && <MarkdownContent content={visibleDocument.content} />}
    </Drawer>
  </div>;
}
