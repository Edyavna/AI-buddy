import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { Button, EmptyState, InlineError } from '@/shared/ui';
import { useAction, useMentorQuery, QueryFeedback, dateLabel } from '@/features/mentor/common';
import { reviewPrepApi, reviewPrepKeys } from './api';
import type { ReviewTemplateItem } from './schemas';
import styles from './ReviewPreparation.module.css';

export function MentorReviewPreparationsPage() {
  const query = useMentorQuery(reviewPrepKeys.mentorList, reviewPrepApi.mentorList);
  return <section className={styles.page}>
    <header className={styles.heading}>
      <h1>Подготовки к ревью</h1>
      <p className={styles.muted}>Карточки стажёров со ссылкой на PR, резюме, чек-листом и результатами проверок.</p>
    </header>
    <TemplateEditor />
    <QueryFeedback query={query} />
    {query.data && (query.data.items.length === 0
      ? <EmptyState title="Подготовок пока нет" description="Когда стажёр отправит подготовку к ревью, карточка появится здесь." />
      : <ul className={styles.cards}>{query.data.items.map(item => <li key={item.id} className={styles.card}>
        <div className={styles.row}>
          <span className={`${styles.tag} ${item.status === 'submitted' ? styles.tagSuccess : styles.tagNeutral}`}>{item.status === 'submitted' ? 'Отправлено' : 'Черновик'}</span>
          <span className={styles.muted}>ревизия {item.revision}{item.submitted_at ? ` · ${dateLabel(item.submitted_at)}` : ''}</span>
        </div>
        <h2><Link to={`/mentor/review-preparations/${encodeURIComponent(item.id)}`}>{item.student.name} — {item.pr.title || `PR #${item.pr.number}`}</Link></h2>
        <p><a href={item.pr.url} target="_blank" rel="noopener noreferrer">{item.pr.repo}#{item.pr.number}</a>
          {item.pr.head_sha && <span className={styles.muted}> · коммит {item.pr.head_sha.slice(0, 7)}</span>}</p>
        {item.pr.fetch_error && <p role="alert" className={styles.warnLine}>{item.pr.fetch_error}</p>}
        <p>{item.answers.summary || 'Резюме не заполнено'}</p>
        {item.answers.questions && <p className={styles.muted}>Вопросы: {item.answers.questions}</p>}
      </li>)}</ul>)}
  </section>;
}

// Наставник настраивает шаблон чек-листа своей команды.
function TemplateEditor() {
  const queryClient = useQueryClient();
  const templateQuery = useMentorQuery(reviewPrepKeys.template, reviewPrepApi.template);
  const action = useAction();
  const [items, setItems] = useState<ReviewTemplateItem[] | null>(null);
  const template = templateQuery.data;
  const editing = items ?? template?.items ?? [];
  if (!template) return null;
  const dirty = items !== null;
  const patch = (next: ReviewTemplateItem[]) => setItems(next);
  const save = () => action.run(async signal => {
    const saved = await reviewPrepApi.saveTemplate(editing, template.revision, signal);
    queryClient.setQueryData(reviewPrepKeys.template, saved);
    setItems(null);
  });
  return <details className={styles.card}>
    <summary><h2 className={styles.inlineH}>Шаблон чек-листа команды</h2> <span className={styles.muted}>{template.custom ? `свой, ревизия ${template.revision}` : 'встроенный по умолчанию'}</span></summary>
    <ul className={styles.checklist}>
      {editing.map((item, index) => <li key={index}>
        <input aria-label="Ключ пункта" value={item.key} placeholder="ключ" onChange={event => patch(editing.map((entry, i) => i === index ? { ...entry, key: event.target.value } : entry))} className={styles.keyInput} />
        <input aria-label="Название пункта" value={item.label} placeholder="Название пункта" onChange={event => patch(editing.map((entry, i) => i === index ? { ...entry, label: event.target.value } : entry))} />
        <label className={styles.checkLine}><input type="checkbox" checked={item.required} onChange={event => patch(editing.map((entry, i) => i === index ? { ...entry, required: event.target.checked } : entry))} /> обязательный</label>
        <Button variant="ghost" size="sm" aria-label="Удалить пункт" onClick={() => patch(editing.filter((_, i) => i !== index))}>Удалить</Button>
      </li>)}
    </ul>
    <div className={styles.row}>
      <Button variant="ghost" size="sm" onClick={() => patch([...editing, { key: `item_${editing.length + 1}`, label: '', required: false }])}>Добавить пункт</Button>
      <Button size="sm" isLoading={action.busy} disabled={!dirty || editing.some(item => !item.key.trim() || !item.label.trim())} onClick={() => void save()}>Сохранить шаблон</Button>
    </div>
    {action.error && <InlineError message={action.error} />}
  </details>;
}
