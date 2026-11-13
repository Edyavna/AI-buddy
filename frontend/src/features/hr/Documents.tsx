import { useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Button, InlineError } from '@/shared/ui';
import { MarkdownContent } from '@/features/documents';
import { useAccessScope, useMe } from '@/features/session';
import { hrApi, type HRDocument } from './api';
import { useOperation, usePrivateQuery } from './requests';
import styles from './HR.module.css';

const labels: Record<HRDocument['status'], string> = {
  draft: 'Черновик', indexing: 'Индексация', ready: 'Опубликован', failed: 'Ошибка индексации',
};

export function HRDocumentsPage() {
  const [selected, setSelected] = useState<string>();
  const key = ['hr', 'documents'];
  const query = usePrivateQuery(key, hrApi.documents, true, 2000);
  const document = query.data?.items.find(item => item.id === selected);
  return <section className={styles.page}>
    <h1>Кадровые инструкции</h1>
    <Button variant="secondary" onClick={() => setSelected('new')}>Новая инструкция</Button>
    {query.isError && <InlineError message={query.error.message} onRetry={() => void query.refetch()} />}
    {query.isPending && <p role="status">Загрузка инструкций…</p>}
    {query.data && (query.data.items.length ? <ul className={styles.list}>
      {query.data.items.map(item => <li className={styles.card} key={item.id}>
        <Button variant="secondary" onClick={() => setSelected(item.id)}>{item.title}</Button>
        <p>{item.revoked ? 'Отозван' : labels[item.status]} · версия {item.version}</p>
      </li>)}
    </ul> : <p>Инструкций пока нет.</p>)}
    {(selected === 'new' || document) && <Editor key={selected} document={document} onSelect={setSelected} />}
  </section>;
}

function Editor({ document, onSelect }: { document: HRDocument | undefined; onSelect: (id: string | undefined) => void }) {
  const me = useMe();
  const scope = useAccessScope(me.data?.id);
  const client = useQueryClient();
  const key = ['hr', 'documents'];
  const op = useOperation(key);
  const [title, setTitle] = useState(document?.title ?? '');
  const [content, setContent] = useState(document?.content ?? '');
  const [teams, setTeams] = useState(document?.team_ids ?? []);
  const [version, setVersion] = useState(document?.version);
  const [preview, setPreview] = useState(false);
  const [fileError, setFileError] = useState('');
  const changed = title !== document?.title || content !== document?.content || teams.join() !== document?.team_ids.join();
  const stale = document && version !== document.version;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const body = { title, content, team_ids: teams };
    const saved = await op.run(body, (operation_id, signal) => document && version
      ? hrApi.editDocument(document.id, { ...body, version }, signal)
      : hrApi.createDocument({ ...body, operation_id }, signal),
    document && version ? async signal => {
      const current = await hrApi.document(document.id, signal);
      return current.version === version + 1 && current.title === title.trim() && current.content === content &&
        [...current.team_ids].sort().join() === [...teams].sort().join() ? current : undefined;
    } : undefined);
    if (!saved) return;
    await client.invalidateQueries({ queryKey: key });
    setTitle(saved.title);
    setContent(saved.content);
    setTeams(saved.team_ids);
    setVersion(saved.version);
    onSelect(saved.id);
  }

  async function publish(action: 'publish' | 'retry' | 'revoke') {
    if (!document) return;
    const updated = await op.run({ action, version: document.version }, (_, signal) => hrApi.publish(document.id, document.version, action, signal),
      async signal => {
        const current = await hrApi.document(document.id, signal);
        return current.version === document.version && (action === 'revoke' ? current.revoked :
          ['indexing', 'ready'].includes(current.status)) ? current : undefined;
      });
    if (updated) await client.invalidateQueries({ queryKey: key });
  }

  async function upload(file?: File) {
    setFileError('');
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.md') || file.size > 1048576) {
      setFileError('Выберите Markdown-файл размером до 1 МБ.');
      return;
    }
    try {
      setContent(await file.text());
      if (!title) setTitle(file.name.slice(0, -3));
    } catch { setFileError('Не удалось прочитать файл.'); }
  }

  return <form className={styles.form} onSubmit={event => void save(event)}>
    <h2>{document ? 'Редактирование инструкции' : 'Новая инструкция'}</h2>
    {document && <p role="status">{document.revoked ? 'Отозван' : labels[document.status]}
      {document.published_version && ` · опубликована версия ${document.published_version}`}</p>}
    {stale && <InlineError message="Документ изменился. Откройте актуальную версию." onRetry={() => {
      setTitle(document.title); setContent(document.content); setTeams(document.team_ids); setVersion(document.version);
    }} />}
    <label>Название<input required maxLength={200} value={title} onChange={event => setTitle(event.target.value)} /></label>
    <label>Загрузить Markdown<input type="file" accept=".md,text/markdown" onChange={event => void upload(event.target.files?.[0])} /></label>
    <label>Текст Markdown<textarea required rows={10} value={content} onChange={event => setContent(event.target.value)} /></label>
    <fieldset><legend>Команды, которым доступна инструкция</legend>
      {scope.data?.teams.map(team => <label key={team.id}>
        <span><input type="checkbox" checked={teams.includes(team.id)} onChange={event =>
          setTeams(event.target.checked ? [...teams, team.id] : teams.filter(id => id !== team.id))} /> {team.name}</span>
      </label>)}
    </fieldset>
    <div className={styles.actions}>
      <Button type="submit" disabled={!teams.length || Boolean(stale)} isLoading={op.pending}>Сохранить черновик</Button>
      <Button type="button" variant="secondary" onClick={() => setPreview(!preview)} aria-expanded={preview}>Предпросмотр</Button>
      {document && !changed && !stale && (document.status === 'draft' || document.revoked) &&
        <Button type="button" disabled={op.pending || document.status === 'indexing'} onClick={() => void publish('publish')}>Опубликовать</Button>}
      {document?.status === 'failed' && <Button type="button" disabled={op.pending} onClick={() => void publish('retry')}>Повторить индексацию</Button>}
      {document && !document.revoked && (document.published_version || document.status === 'indexing') &&
        <Button type="button" variant="secondary" disabled={op.pending} onClick={() => void publish('revoke')}>Отозвать публикацию</Button>}
      <Button type="button" variant="secondary" onClick={() => onSelect(undefined)}>Закрыть редактор</Button>
    </div>
    {preview && <div className={styles.card}><MarkdownContent content={content} /></div>}
    {(op.error || fileError || document?.error) && <InlineError message={op.error || fileError || document?.error || ''} />}
  </form>;
}
