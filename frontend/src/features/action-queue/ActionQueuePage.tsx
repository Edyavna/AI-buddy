import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { accessDenied, dateLabel, QueryFeedback, useAction } from '@/features/mentor/common';
import { queryRequestSignal } from '@/shared/api/query-access';
import { Button, EmptyState, InlineError } from '@/shared/ui';
import { completeInvitation, kinds, loadQueue, pageSize, queueKey, type Kind, type QueueItem } from './api';
import styles from './ActionQueue.module.css';
import { actionQueueSignal, clearActionQueueAccess, useActionQueueQuery } from './access';

const labels = { invitation: 'Приглашения', confirmation: 'Подтверждения', question: 'Вопросы' };
const reasons = {
  record_invitation: 'Стажёр ждёт приглашения в закрытые чаты',
  review_checkpoint: 'Чекпоинт ожидает подтверждения наставника',
  claim_question: 'Кадровое обращение ждёт ответственного',
  answer_question: 'Вопрос ожидает ответа или завершения',
};
const linkLabels = { review_checkpoint: 'К чекпоинту', claim_question: 'Взять обращение', answer_question: 'Открыть вопрос' };

function QueueCard({ item, role, busy, complete }: {
  item: QueueItem;
  role: 'mentor' | 'hr';
  busy: boolean;
  complete: (item: QueueItem, note: string) => void;
}) {
  const checkpoint = `/mentor/interns/${encodeURIComponent(item.intern.id)}#step-${encodeURIComponent(item.step_id ?? '')}`;
  const [note, setNote] = useState('');
  const since = dateLabel(item.since), updated = dateLabel(item.updated_at);
  return <li className={styles.card}>
    <div>
      <span className={styles.category}>{labels[item.kind]}</span>
      <h2>{item.intern.name}</h2>
      {item.step_title && <p>{item.step_title}</p>}
      {item.question_preview && <p>{item.question_preview}</p>}
      <p>{reasons[item.action]}</p>
      <p className={styles.date}>
        Ожидает с <time dateTime={item.since}>{since}</time>
        {updated !== since && <> · Обновлено <time dateTime={item.updated_at}>{updated}</time></>}
      </p>
    </div>
    {item.action === 'record_invitation' ? <form className={styles.invitation} onSubmit={event => {
      event.preventDefault();
      complete(item, note);
    }}>
      <Link to={checkpoint}>К чекпоинту</Link>
      <label>Пояснение к приглашению (необязательно)
        <textarea rows={2} maxLength={500} value={note} onChange={event => setNote(event.target.value)} />
      </label>
      <Button type="submit" disabled={busy}>Отметить ручное приглашение</Button>
    </form> : <Link className={styles.action} to={item.action === 'review_checkpoint' ? checkpoint : `/${role}/questions/${encodeURIComponent(item.id)}`}>
      {linkLabels[item.action]}
    </Link>}
  </li>;
}

export function ActionQueuePage({ role }: { role: 'mentor' | 'hr' }) {
  const client = useQueryClient();
  const action = useAction();
  const title = useRef<HTMLHeadingElement>(null);
  const [intern, setIntern] = useState('');
  const [kind, setKind] = useState<Kind | ''>('');
  const [offset, setOffset] = useState(0);
  const [status, setStatus] = useState('');
  const key = [...queueKey, role, intern, kind, offset];
  const query = useActionQueueQuery(key, signal => loadQueue(intern, kind, offset, signal), 0);
  const page = query.isError ? undefined : query.data;
  const total = page?.total;

  useEffect(() => {
    if (offset > 0 && total !== undefined && offset >= total) {
      setOffset(Math.floor(Math.max(0, total - 1) / pageSize) * pageSize);
    }
  }, [offset, total]);

  function complete(item: QueueItem, note: string) {
    setStatus('');
    void action.run(async owner => {
      const signal = actionQueueSignal(client, queryRequestSignal(client, key, owner));
      try {
        await completeInvitation(item.intern.id, item.id, note, signal);
        signal.throwIfAborted();
        setStatus('Ручное приглашение отмечено.');
        title.current?.focus();
      } catch (error) {
        if (signal.aborted) return;
        if (accessDenied(error)) clearActionQueueAccess(client);
        throw error;
      } finally {
        if (!signal.aborted) await client.invalidateQueries({ queryKey: queueKey });
      }
    });
  }

  return <section className={styles.page} aria-labelledby="action-queue-title">
    <h1 id="action-queue-title" ref={title} tabIndex={-1}>Требуется моё действие</h1>
    <div className={styles.filters}>
      <label>Стажёр
        <select value={intern} onChange={event => { setIntern(event.target.value); setOffset(0); }}>
          <option value="">Все стажёры</option>
          {intern && !page?.interns.some(person => person.id === intern) && <option value={intern}>Выбранный стажёр</option>}
          {page?.interns.map(person => <option key={person.id} value={person.id}>{person.name}</option>)}
        </select>
      </label>
      <label>Тип действия
        <select value={kind} onChange={event => { setKind(event.target.value as Kind | ''); setOffset(0); }}>
          <option value="">Все действия</option>
          {(role === 'hr' ? ['question'] as const : kinds).map(type =>
            <option key={type} value={type}>{labels[type]}</option>)}
        </select>
      </label>
    </div>
    <QueryFeedback query={query} />
    {action.error && <InlineError message={action.error} />}
    {status && <p role="status">{status}</p>}
    {page && <>
      <p className={styles.counts} aria-live="polite">
        {(role === 'hr' ? ['question'] as const : kinds).map(type =>
          <span key={type}>{labels[type]} <strong>{page.counts[type]}</strong></span>)}
      </p>
      {page.items.length === 0 ? <EmptyState title="Нет ожидающих действий" description="Новые действия появятся здесь." />
        : <ol className={styles.list}>{page.items.map(item =>
          <QueueCard key={`${item.kind}:${item.id}`} item={item} role={role} busy={action.busy} complete={complete} />)}
        </ol>}
      {page.total > pageSize && <nav className={styles.pagination} aria-label="Страницы очереди">
        <Button variant="secondary" disabled={offset === 0} onClick={() => setOffset(value => value - pageSize)}>Назад</Button>
        <span>{offset + 1}–{offset + page.items.length} из {page.total}</span>
        <Button variant="secondary" disabled={offset + pageSize >= page.total} onClick={() => setOffset(value => value + pageSize)}>Далее</Button>
      </nav>}
    </>}
  </section>;
}
