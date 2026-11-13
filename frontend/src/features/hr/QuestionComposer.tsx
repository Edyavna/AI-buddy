import { useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Button, InlineError } from '@/shared/ui';
import { hrApi, directionLabel, questionStatus, type Direction, type Draft, type RoutedQuestion } from './api';
import { useOperation, usePrivateQuery } from './requests';
import styles from './HR.module.css';

const directions: Direction[] = ['mentor', 'hr'];

export function QuestionComposer({ messageId, onOpenDocument }: {
  messageId: string; onOpenDocument: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  return <section>
    <Button variant="secondary" size="sm" onClick={() => setOpen(!open)} aria-expanded={open}>
      {open ? 'Свернуть обращения' : 'Обратиться к специалисту'}
    </Button>
    {open && <MessageQuestions messageId={messageId} onOpenDocument={onOpenDocument} />}
  </section>;
}

function MessageQuestions({ messageId, onOpenDocument }: {
  messageId: string; onOpenDocument: (id: string) => void;
}) {
  const key = ['hr', 'message', messageId];
  const query = usePrivateQuery(key, signal => hrApi.message(messageId, signal), true, 5000);
  const [edit, setEdit] = useState<{ draft: Draft; question?: RoutedQuestion }>();
  const [historyId, setHistoryId] = useState('');
  const history = usePrivateQuery(['hr', 'history', historyId], signal => hrApi.history(historyId, signal), Boolean(historyId));
  const client = useQueryClient();
  const op = useOperation(key);

  if (!query.data) return query.isError
    ? <InlineError message={query.error.message} onRetry={() => void query.refetch()} />
    : <p role="status">Загрузка обращений…</p>;

  const data = query.data;
  const suggested = data.routing?.direction;
  const remaining = directions.filter(direction => !data.items.some(q => q.original_direction === direction));
  const defaultDirection = suggested && suggested !== 'needs_clarification' && remaining.includes(suggested)
    ? suggested : (data.drafts.find(d => remaining.includes(d.direction))?.direction ?? remaining[0]);
  const current: typeof edit = edit ?? (defaultDirection ? { draft: data.drafts.find(d => d.direction === defaultDirection) ??
    { direction: defaultDirection, question: data.items.length && data.routing?.split ? '' : data.question, context_excerpt: '' } } : undefined);

  function update(draft: Draft) { setEdit({ ...current, draft }); }

  async function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!current) return;
    const { draft, question } = current;
    const saved = await op.run({ draft, question: question?.id, revision: question?.revision }, (operation_id, signal) => question
      ? hrApi.reroute(question.id, { ...draft, operation_id, revision: question.revision }, signal)
      : hrApi.send(messageId, { ...draft, operation_id }, signal),
    async signal => (await hrApi.message(messageId, signal)).items.find(q => (question ? q.id === question.id && q.revision > question.revision : q.original_direction === draft.direction)
      && q.direction === draft.direction && q.question === draft.question && q.context_excerpt === draft.context_excerpt));
    if (!saved) { await client.invalidateQueries({ queryKey: key }); return; }
    setEdit(undefined);
    await client.invalidateQueries({ queryKey: key });
    if (question) await client.invalidateQueries({ queryKey: ['hr', 'history', question.id] });
  }

  async function saveDraft(draft: Draft) {
    const saved = await op.run(draft, (_, signal) => hrApi.draft(messageId, draft, signal));
    if (saved) await client.invalidateQueries({ queryKey: key });
  }

  return <div className={styles.page}>
    {data.routing ? <p>{suggested === 'needs_clarification' ? data.routing.clarification :
      `Предложенный адресат — ${directionLabel[suggested ?? 'mentor']}. ${data.routing.reason}`}
      {data.routing.split && ' Можно отправить две части по отдельности, отредактировав текст каждой.'}</p> :
      <p>Выберите, кому отправить вопрос.</p>}
    {data.items.map(q => <div className={styles.card} key={q.id}>
      <strong>{directionLabel[q.direction]} · {questionStatus[q.status]}</strong>
      <p>{q.question}</p>
      {q.resolution && <p>{q.resolution}</p>}
      {q.document_ids.map(id => <Button key={id} size="sm" variant="secondary" onClick={() => onOpenDocument(id)}>Открыть инструкцию</Button>)}
      <div className={styles.actions}>
        <Button variant="secondary" size="sm" onClick={() => setEdit({ question: q, draft: {
          direction: q.direction === 'hr' ? 'mentor' : 'hr', question: q.question, context_excerpt: '',
        } })}>Изменить адресата</Button>
        <Button variant="secondary" size="sm" onClick={() => setHistoryId(historyId === q.id ? '' : q.id)}>История</Button>
      </div>
      {historyId === q.id && (history.isError ? <InlineError message={history.error.message} onRetry={() => void history.refetch()} /> :
        <ol>{history.data?.history.map(entry => <li key={entry.revision}>
          {entry.snapshot.direction === 'hr' ? 'HR' : 'Наставник'}
          {typeof entry.snapshot.resolution === 'string' && <p>{entry.snapshot.resolution}</p>}
        </li>)}</ol>)}
    </div>)}
    {current && <form onSubmit={event => void send(event)} className={styles.form}>
      <label>Адресат<select value={current.draft.direction} onChange={event => {
        const direction = event.target.value as Direction;
        update(current.question ? { ...current.draft, direction } : data.drafts.find(d => d.direction === direction) ?? { ...current.draft, direction });
      }}>
        {(current.question ? directions.filter(d => d !== current.question?.direction) : remaining)
          .map(direction => <option value={direction} key={direction}>{directionLabel[direction]}</option>)}
      </select></label>
      <label>Вопрос<textarea required rows={3} value={current.draft.question} onChange={event => update({ ...current.draft, question: event.target.value })} /></label>
      <label>Контекст для специалиста<textarea rows={3} value={current.draft.context_excerpt} onChange={event => update({ ...current.draft, context_excerpt: event.target.value })} /></label>
      {!data.available_directions.includes(current.draft.direction) && <p role="status">
        {current.draft.direction === 'hr' ? 'Для вашей команды пока не назначен HR.' : 'Вам пока не назначен наставник.'}
      </p>}
      <div className={styles.actions}>
        <Button type="submit" isLoading={op.pending} disabled={!data.available_directions.includes(current.draft.direction)}>
          {current.question ? 'Подтвердить перенаправление' : 'Подтвердить отправку'}
        </Button>
        {!current.question && <Button type="button" variant="secondary" disabled={op.pending || !current.draft.question.trim()}
          onClick={() => void saveDraft(current.draft)}>Сохранить черновик</Button>}
        {current.question && <Button type="button" variant="secondary" onClick={() => setEdit(undefined)}>Отмена</Button>}
      </div>
    </form>}
    {op.error && <InlineError message={op.error} />}
  </div>;
}
