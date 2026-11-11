import { useCallback, useRef, useState, type KeyboardEvent } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { Button, Drawer, EmptyState, InlineError, TextField } from '@/shared/ui';
import { MarkdownContent } from '@/features/documents';
import { mentorApi, mentorKeys } from './api';
import { useAction, useMentorQuery, QueryFeedback, documentLabels, dateLabel } from './common';
import styles from './Mentor.module.css';

export function MentorDocumentViewer({ id, onClose }: { id: string; onClose: () => void }) {
  const query = useMentorQuery(mentorKeys.document(id), signal => mentorApi.document(id, signal));
  const root = useRef<HTMLDivElement>(null), closeRef = useRef(onClose); closeRef.current = onClose;
  const close = useCallback(() => closeRef.current(), []);
  function trap(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Tab') return;
    const controls = root.current?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], [tabindex="0"]');
    const first = controls?.[0], last = controls?.[controls.length - 1];
    if (!first || !last) return;
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }
  return <div className={styles.host} ref={root} onKeyDown={trap}><Drawer isOpen onClose={close} title={query.data?.title ?? 'Документ'} width="lg"><QueryFeedback query={query} />{query.data && <MarkdownContent content={query.data.content} />}</Drawer></div>;
}
export const readFileBytes = (file: File) => typeof file.arrayBuffer === 'function' ? file.arrayBuffer() : new Promise<ArrayBuffer>((resolve, reject) => {
  const reader = new FileReader(); reader.onload = () => resolve(reader.result as ArrayBuffer); reader.onerror = () => reject(new Error('Не удалось прочитать файл')); reader.readAsArrayBuffer(file);
});
export async function validateMarkdown(file: File | undefined, title: string) {
  if (!title.trim()) throw new Error('Укажите название документа');
  if (!file || !/\.md$/i.test(file.name)) throw new Error('Выберите один файл .md');
  if (file.size > 1048576) throw new Error('Размер файла должен быть не больше 1 MiB');
  let text: string;
  try { text = new TextDecoder('utf-8', { fatal: true }).decode(await readFileBytes(file)); }
  catch { throw new Error('Файл должен быть в кодировке UTF-8'); }
  if (!text.trim()) throw new Error('Файл не должен быть пустым');
  return file;
}
type Attempt = { id: string; file: File; title: string; questionId: string | null };
export function MentorDocumentsPage() {
  const [params] = useSearchParams();
  const questionId = params.get('question_id');
  const query = useMentorQuery(mentorKeys.documents, mentorApi.documents);
  const client = useQueryClient(), action = useAction();
  const [view, setView] = useState<string | null>(null), [title, setTitle] = useState(''), [file, setFile] = useState<File>();
  const [attempt, setAttempt] = useState<Attempt | null>(null), [notice, setNotice] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);
  function upload() {
    void action.run(async signal => {
      const pending = attempt ?? { id: crypto.randomUUID(), file: await validateMarkdown(file, title), title: title.trim(), questionId };
      if (signal.aborted) return;
      setAttempt(pending); setNotice('');
      const form = new FormData(); form.set('upload_id', pending.id); form.set('title', pending.title); form.set('file', pending.file);
      if (pending.questionId) form.set('question_id', pending.questionId);
      try {
        await mentorApi.upload(form, signal);
        if (signal.aborted) return;
        setAttempt(null); setTitle(''); setFile(undefined); if (fileInput.current) fileInput.current.value = '';
        setNotice('Документ принят. Дождитесь завершения индексации.');
        await client.invalidateQueries({ queryKey: mentorKeys.all });
      } catch (e) {
        if (signal.aborted) return;
        // A network/5xx/protocol failure may happen AFTER the server accepted the upload.
        // Preserve the complete immutable request until an explicit retry confirms it.
        if (e instanceof ClientError && e.kind === 'http' && e.status && e.status < 500) {
          setAttempt(null);
          if ([401, 403, 404].includes(e.status)) await query.refetch();
        }
        throw e;
      }
    });
  }
  function retry(id: string) {
    void action.run(async signal => { await mentorApi.retryDocument(id, signal); if (!signal.aborted) await client.invalidateQueries({ queryKey: mentorKeys.all }); });
  }
  return <section className={styles.page}><header className={styles.heading}><h1>Знания команды</h1><p className={styles.muted}>Добавляйте инструкции, чтобы помощник отвечал по проверенным источникам.</p></header>
    <form className={styles.card} onSubmit={e => { e.preventDefault(); upload(); }}><h2>Добавить Markdown-документ</h2>{questionId && <p>Документ будет связан с выбранным вопросом. Сам вопрос останется открытым.</p>}
      <TextField label="Название документа" value={title} disabled={action.busy || !!attempt} onChange={e => setTitle(e.target.value)} required />
      <label className={styles.field}>Файл Markdown<input ref={fileInput} type="file" accept=".md,text/markdown" disabled={action.busy || !!attempt} onChange={e => setFile(e.target.files?.[0])} aria-describedby="markdown-help" /></label>
      <p id="markdown-help" className={styles.muted}>Один .md файл в UTF-8, до 1 MiB.</p>
      {attempt && <p role="status">Результат загрузки ещё не подтверждён. Повтор использует тот же файл и идентификатор, чтобы не создать копию.</p>}
      <Button type="submit" isLoading={action.busy}>{attempt ? 'Повторить ту же загрузку' : 'Загрузить документ'}</Button>
    </form>
    {action.error && <InlineError message={action.error} />}{notice && <p role="status">{notice}</p>}
    <QueryFeedback query={query} />
    {query.data && (query.data.items.length ? <ul className={styles.list}>{query.data.items.map(doc => <li key={doc.id} className={styles.card}><span className={styles.tag}>{documentLabels[doc.status]}</span><h2>{doc.title}</h2><p className={styles.muted}>{doc.author.name} · {dateLabel(doc.created_at)}</p>{doc.error && <p role="alert">{doc.error}</p>}<div className={styles.row}><Button variant="secondary" onClick={() => setView(doc.id)}>Просмотреть {doc.title}</Button>{doc.status === 'failed' && <Button onClick={() => retry(doc.id)} isLoading={action.busy}>Повторить индексацию</Button>}</div></li>)}</ul> : <EmptyState title="Документов пока нет" description="Загрузите первую инструкцию команды." />)}
    {view && <MentorDocumentViewer id={view} onClose={() => setView(null)} />}
  </section>;
}
