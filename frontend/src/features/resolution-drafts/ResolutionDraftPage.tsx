import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { queryRequestSignal, revokeQueryAccess } from '@/shared/api/query-access';
import { Button, InlineError, TextField } from '@/shared/ui';
import { MarkdownContent, DocumentDrawer } from '@/features/documents';
import { useMe, useAccessScope } from '@/features/session';
import { useAction, useMentorQuery, QueryFeedback, accessDenied } from '@/features/mentor/common';
import { MentorDocumentViewer } from '@/features/mentor/documents';
import { draftKey, resolutionApi, groupResolutionApi, type Direction, type Draft, type Workspace, type SaveDraft } from './api';
import styles from './ResolutionDraft.module.css';

const statusLabels = { draft: 'Черновик · виден только вам', indexing: 'Индексируется', ready: 'Опубликован и готов к поиску', failed: 'Ошибка индексации' };
const uncertain = (error: unknown) => error instanceof ClientError && (['network', 'aborted', 'protocol'].includes(error.kind) || (error.status ?? 0) >= 500);

export function ResolutionDraftPage({ direction, group = false }: { direction: Direction; group?: boolean }) {
  const { id = '' } = useParams();
  const me = useMe();
  const scope=useAccessScope(me.data?.id);
  const teamNames=Object.fromEntries((scope.data?.teams ?? []).map(team=>[team.id,team.name]));
  const api = group ? groupResolutionApi : resolutionApi;
  const query = useMentorQuery(draftKey(direction, id, group), signal => api.workspace(direction, id, signal));
  return <section className={styles.page}>
    <Link to={`/${direction}/${group ? 'question-groups' : 'questions'}/${encodeURIComponent(id)}`}>{group ? '← К группе вопросов' : '← К исходному вопросу'}</Link>
    <header><p className={styles.eyebrow}>Знания команды</p><h1>Инструкция из решения</h1><p>Превратите проверенный ответ в материал, который поможет другим стажёрам.</p></header>
    <QueryFeedback query={query} />
    {!query.data && !query.isPending && !query.isError && <InlineError message="Черновик недоступен. Проверьте доступ к исходному вопросу." onRetry={() => void query.refetch()} />}
    {query.data && <Editor key={`${direction}:${id}:${me.data?.id}:${group}`} direction={direction} id={id} workspace={query.data} teamNames={teamNames} api={api} group={group} />}
  </section>;
}
function Editor({ direction, id, workspace,teamNames,api,group }: { direction: Direction; id: string; workspace: Workspace;teamNames:Record<string,string>;api:typeof resolutionApi;group:boolean }) {
  const client = useQueryClient(), action = useAction();
  const key = draftKey(direction, id, group);
  const initial = workspace.draft;
  const [title, setTitle] = useState(initial?.title ?? 'Инструкция команды');
  const [content, setContent] = useState(initial?.content ?? '');
  const [target, setTarget] = useState(initial?.target_document_id ?? '');
  const [baseVersion, setBaseVersion] = useState<number | null>(initial?.base_version ?? null);
  const [baseContent, setBaseContent] = useState(initial?.base_content ?? '');
  const [audience,setAudience]=useState(initial?.team_ids ?? [workspace.source.team_id]);
  const [revision, setRevision] = useState(initial?.revision ?? 0);
  const [dirty, setDirty] = useState(!initial), [reviewed, setReviewed] = useState(false);
  const [notice, setNotice] = useState(''), [conflict, setConflict] = useState(false), [unknown, setUnknown] = useState(false);
  const [open, setOpen] = useState(false);
  const [operationLabel,setOperationLabel]=useState('Выполняем действие…');
  const draft = workspace.draft;
  const published = !!draft?.document_id;
  function changed() { setDirty(true); setReviewed(false); setNotice(''); }
  function accept(next: Draft) {
    setRevision(next.revision); setDirty(false); setConflict(false); setUnknown(false);
    client.setQueryData(key, { ...workspace, draft: next });
  }
  function run(work: (signal: AbortSignal) => Promise<void>,label='Выполняем действие…') {
    setOperationLabel(label);
    void action.run(async outer => {
      const scope = AbortSignal.any([outer, queryRequestSignal(client, key)]);
      try { await work(scope); scope.throwIfAborted(); }
      catch (error) {
        if (!scope.aborted && accessDenied(error)) {
          revokeQueryAccess(client, key);
          await client.cancelQueries({ queryKey: key }, { revert: false }); client.setQueryData(key, null);
        }
        if (error instanceof ClientError && error.status === 409) setConflict(true);
        throw error;
      }
    });
  }
  const timed = (signal: AbortSignal) => AbortSignal.any([signal, AbortSignal.timeout(10_000)]);
  async function choose(targetId: string, signal: AbortSignal) {
    if (!targetId) { setTarget(''); setBaseVersion(null); setBaseContent(''); setAudience([workspace.source.team_id]); changed(); return; }
    const next = await api.target(direction, id, targetId, timed(signal)); signal.throwIfAborted();
    setTarget(next.id); setBaseVersion(next.version); setBaseContent(next.content); if (targetId !== target) setTitle(next.title); setAudience(next.team_ids); setConflict(false); changed();
  }
  function save() {
    const body: SaveDraft = { title, content, revision, target_document_id: target || null, base_version: baseVersion };
    run(async signal => {
      let next: Draft;
      try { next = await api.save(direction, id, body, timed(signal)); }
      catch (error) {
        signal.throwIfAborted(); if (!uncertain(error)) throw error;
        setUnknown(true);
        const latest = await api.workspace(direction, id, timed(signal));
        if (!latest.draft || latest.draft.title !== title.trim() || latest.draft.content !== content || latest.draft.target_document_id !== body.target_document_id || latest.draft.revision !== revision + 1) throw error;
        next = latest.draft;
      }
      signal.throwIfAborted(); accept(next); setNotice('Черновик сохранён. После обновления страницы он останется здесь.');
    });
  }
  function publish(retry = false) {
    run(async signal => {
      let next: Draft;
      try { next = await api.publish(direction, id, revision, retry, timed(signal)); }
      catch (error) {
        signal.throwIfAborted(); if (!uncertain(error)) throw error;
        setUnknown(true);
        const latest = await api.workspace(direction, id, timed(signal));
        if (!latest.draft?.document_id || (retry && latest.draft.status === 'failed')) throw error;
        next = latest.draft;
      }
      signal.throwIfAborted(); accept(next); setNotice('Публикация принята. Ссылка появится в вопросе после индексации.');
      await client.invalidateQueries({ queryKey: group ? ['question-groups',direction,id] : [direction, 'question', id] });
    },'Публикуем инструкцию…');
  }
  function reconcile() {
    run(async signal => {
      const next = await api.workspace(direction, id, timed(signal)); signal.throwIfAborted();
      client.setQueryData(key, next); setUnknown(false);
      if (next.draft) setRevision(next.draft.revision);
      setConflict(false); setDirty(true); setReviewed(false);
      setNotice('Версия перечитана. Ваш текст сохранён в редакторе. Проверьте его перед новой записью.');
    });
  }
  return <>
    <details className={styles.card}><summary>Источник: {direction === 'hr' ? 'ответ HR' : 'ответ наставника'}</summary><dl><dt>Вопрос</dt><dd>{workspace.source.question}</dd><dt>Проверенный ответ</dt><dd>{workspace.source.resolution}</dd><dt>Чекпоинт</dt><dd>{workspace.source.step_id ?? 'Не указан'}</dd></dl></details>
    <div className={styles.card}><span role="status">{statusLabels[draft?.status ?? 'draft']}</span><p>Перед публикацией удалите имена, контакты, личные обстоятельства и секреты. Исходный вопрос остаётся приватным.</p><p>Аудитория инструкции — стажёры команд: {audience.map(team=>teamNames[team] ?? team).join(', ')}.</p></div>
    {!published && <form onSubmit={event => { event.preventDefault(); save(); }} className={styles.card}>
      <div className={styles.actions}><h2>Редактор Markdown</h2><Button variant="secondary" type="button" disabled={action.busy || unknown || conflict} onClick={() => run(async signal => {
        const next = await api.generate(direction, id, timed(signal)); signal.throwIfAborted(); setTitle(next.title); setContent(next.content); changed(); setNotice(next.mode === 'demo' ? 'Демонстрационное предложение. Проверьте и отредактируйте текст.' : 'Предложение помощника. Проверьте и отредактируйте текст.');
      })}>{content ? 'Заменить текст предложением помощника' : 'Предложить текст'}</Button></div>
      <label className={styles.field}>Куда опубликовать<select value={target} disabled={action.busy || unknown} onChange={event => run(signal => choose(event.target.value, signal))}><option value="">Новый документ</option>{workspace.targets.map(t => <option key={t.id} value={t.id}>{t.title} · версия {t.version}</option>)}</select></label>
      <TextField label="Название инструкции" required maxLength={200} value={title} disabled={action.busy || unknown} onChange={event => { setTitle(event.target.value); changed(); }} />
      <label className={styles.field}>{target ? 'Дополнение к документу' : 'Текст инструкции'}<textarea rows={10} maxLength={1_048_576} value={content} disabled={action.busy || unknown} onChange={event => { setContent(event.target.value); changed(); }} /></label>
      <div className={styles.actions}><Button type="submit" disabled={!dirty || action.busy || conflict || unknown}>Сохранить черновик</Button><span>{dirty ? 'Есть несохранённые изменения' : 'Все изменения сохранены'}</span></div>
    </form>}
    {!published && <section className={styles.card} aria-label="Предпросмотр инструкции"><h2>Предпросмотр публикации</h2>{baseContent && <details><summary>Существующий текст · версия {baseVersion}</summary><MarkdownContent content={baseContent} /></details>}<h3>{target ? 'Добавится в конец документа' : title}</h3>{content ? <MarkdownContent content={content} /> : <p>Напишите текст или запросите предложение помощника. При ошибке генерации можно сохранить ручной текст.</p>}
      <label className={styles.check}><input type="checkbox" checked={reviewed} disabled={action.busy || dirty} onChange={event => setReviewed(event.target.checked)} />Я проверил текст и аудиторию, удалил личные сведения и секреты</label>
      <Button disabled={!reviewed || dirty || !content.trim() || !draft || action.busy || conflict || unknown} onClick={() => publish()}>Опубликовать инструкцию</Button>
    </section>}
    {published && <section className={styles.card}><h2>{draft.title}</h2><p>Версия {draft.document_version}. {statusLabels[draft.status]}</p>{draft.error && <InlineError message={draft.error} />}{draft.status === 'failed' && <Button disabled={action.busy || unknown} onClick={() => publish(true)}>Повторить индексацию</Button>}{draft.status === 'ready' && <Button onClick={() => setOpen(true)}>Открыть опубликованную инструкцию</Button>}</section>}
    {action.busy && <p role="status">{operationLabel}</p>}{notice && <p role="status">{notice}</p>}{action.error && <InlineError message={action.error} />}
    {(conflict || unknown) && <div className={styles.card} role="alert"><p>{conflict ? 'Версия изменилась. Перечитайте документ или черновик; автоматической перезаписи не будет.' : 'Результат операции пока неизвестен. Сначала сверьте состояние с сервером.'}</p><Button variant="secondary" disabled={action.busy} onClick={reconcile}>Сверить с сервером</Button>{target && <Button variant="secondary" disabled={action.busy} onClick={() => run(signal => choose(target, signal))}>Обновить исходный документ</Button>}</div>}
    {open && draft?.document_id && (direction === 'mentor' ? <MentorDocumentViewer id={draft.document_id} onClose={() => setOpen(false)} /> : <DocumentDrawer documentId={draft.document_id} onClose={() => setOpen(false)} />)}
  </>;
}
