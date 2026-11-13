import { useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { Button, EmptyState, InlineError } from '@/shared/ui';
import { mentorApi, mentorKeys, type QuestionUpdate } from './api';
import { useAction, useMentorQuery, QueryFeedback, questionLabels, dateLabel } from './common';
import { MentorDocumentViewer } from './documents';
import type { QuestionStatus } from './models';
import styles from './Mentor.module.css';

export function MentorQuestionsPage() {
  const [params, setParams] = useSearchParams();
  const status = params.get('status') ?? '';
  const query = useMentorQuery(mentorKeys.questions(status), signal => mentorApi.questions(status, signal));
  return <section className={styles.page}><header className={styles.heading}><h1>Вопросы без ответа</h1><p className={styles.muted}>Помогите стажёрам и дополните знания команды.</p></header>
    <label className={styles.field}>Статус вопроса<select value={status} onChange={e => setParams(e.target.value ? { status: e.target.value } : {})}><option value="">Все вопросы</option>{Object.entries(questionLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <QueryFeedback query={query} />
    {query.data && (query.data.items.length ? <ul className={styles.list}>{query.data.items.map(q => <li key={q.id} className={styles.card}><span className={styles.tag}>{questionLabels[q.status]}</span><h2><Link to={`/mentor/questions/${encodeURIComponent(q.id)}`}>{q.question}</Link></h2><p className={styles.muted}>{q.student.name} · {dateLabel(q.created_at)}</p></li>)}</ul> : <EmptyState title="Вопросов пока нет" description="Новые вопросы появятся здесь." />)}
  </section>;
}
export function MentorQuestionPage() {
  const { id = '' } = useParams();
  // Remount state when a direct navigation selects another question.
  return <QuestionDetail key={id} id={id} />;
}
function QuestionDetail({ id }: { id: string }) {
  const query = useMentorQuery(mentorKeys.question(id), signal => mentorApi.question(id, signal));
  const docs = useMentorQuery(mentorKeys.documents, mentorApi.documents);
  const client = useQueryClient(), action = useAction();
  const [comment, setComment] = useState(''), [selected, setSelected] = useState<string[]>([]), [view, setView] = useState<string | null>(null);
  const q = query.data, ready = docs.data?.items.filter(d => d.status === 'ready') ?? [];
  const canResolve = !!comment.trim() && selected.length > 0 && selected.every(id => ready.some(d => d.id === id));
  function update(status: QuestionStatus) {
    if (!q || action.busy) return;
    const body: QuestionUpdate = { status, revision: q.revision, ...(status === 'resolved' && { resolution: comment.trim(), document_ids: selected }) };
    void action.run(async signal => {
      try {
        const next = await mentorApi.updateQuestion(id, body, signal);
        if (signal.aborted) return;
        client.setQueryData(mentorKeys.question(id), next);
        await client.invalidateQueries({ queryKey: mentorKeys.all });
      } catch (e) {
        if (signal.aborted) return;
        if (e instanceof ClientError && e.status === 409) {
          await Promise.all([query.refetch(), docs.refetch()]);
          throw new Error('Данные изменились. Проверьте обновлённый вопрос и документы, затем повторите действие.');
        }
        if (e instanceof ClientError && [401, 403, 404].includes(e.status ?? 0)) await query.refetch();
        throw e;
      }
    });
  }
  return <section className={styles.page}><Link to="/mentor/questions">← Все вопросы</Link><h1>Вопрос стажёра</h1><QueryFeedback query={query} />
    {q && <><article className={styles.card}><span className={styles.tag}>{questionLabels[q.status]}</span><h2>{q.question}</h2><p>{q.student.name} · {dateLabel(q.created_at)}</p><p className={styles.muted}>Шаг плана: {q.step_id ? <Link to={`/mentor/interns/${encodeURIComponent(q.student.id)}`}>Открыть план стажёра</Link> : 'Не указан'}</p><h3>Контекст сообщения</h3><p>{q.context_excerpt}</p>{q.resolution && <p>Решение: {q.resolution}</p>}
      <div className={styles.row}><Button variant="secondary" disabled={q.status === 'in_progress'} isLoading={action.busy} onClick={() => update('in_progress')}>{q.status === 'resolved' ? 'Открыть повторно' : 'Взять в работу'}</Button><Link to={`/mentor/documents?question_id=${encodeURIComponent(id)}`}>Добавить документ</Link></div></article>
      <section className={styles.card}><h2>Связанные документы</h2><QueryFeedback query={docs} />{q.document_ids.length ? q.document_ids.map(docId => { const doc = docs.data?.items.find(d => d.id === docId); return doc && <Button key={docId} variant="ghost" onClick={() => setView(docId)}>{doc.title}</Button>; }) : <p className={styles.muted}>Документы ещё не добавлены.</p>}</section>
      {q.status !== 'resolved' && <form className={styles.card} onSubmit={e => { e.preventDefault(); if (canResolve) update('resolved'); }}><h2>Решить вопрос</h2><label className={styles.field}>Комментарий наставника<textarea required value={comment} onChange={e => setComment(e.target.value)} /></label><fieldset><legend>Готовые документы для решения</legend>{ready.length ? ready.map(doc => <label key={doc.id} className={styles.check}><input type="checkbox" checked={selected.includes(doc.id)} onChange={e => setSelected(current => e.target.checked ? [...current, doc.id] : current.filter(id => id !== doc.id))} />{doc.title}</label>) : <p>Дождитесь готовности хотя бы одного документа.</p>}</fieldset><p className={styles.muted}>Загрузка документа не закрывает вопрос. Подтвердите решение вручную.</p><Button type="submit" disabled={!canResolve} isLoading={action.busy}>Подтвердить решение</Button></form>}
      {q.status === 'resolved' && <section className={styles.card}><h2>Сохранить решение в базе знаний</h2><p>Создайте редактируемую инструкцию из ответа. Публикация потребует вашего подтверждения.</p><Link to={`/mentor/questions/${encodeURIComponent(id)}/instruction`}>Подготовить инструкцию</Link></section>}
    </>}{action.error && <InlineError message={action.error} />}
    {view && q && <MentorDocumentViewer id={view} onClose={() => setView(null)} />}
  </section>;
}
