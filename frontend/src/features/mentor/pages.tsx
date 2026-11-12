import { useCallback, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Button, EmptyState, InlineError } from '@/shared/ui';
import { AssignInternDialog } from './AssignInternDialog';
import { ClientError } from '@/shared/api';
import { mentorApi, mentorKeys } from './api';
import { useMentorQuery, QueryFeedback } from './common';
import styles from './Mentor.module.css';

const stepLabels = { not_started: 'Не начат', in_progress: 'В работе', blocked: 'Заблокирован', completed: 'Завершён' };
export function MentorInternsPage() {
  const query = useMentorQuery(mentorKeys.interns, mentorApi.interns);
  const [adding, setAdding] = useState(false), [notice, setNotice] = useState('');
  const close = useCallback(() => setAdding(false), []);
  const assigned = useCallback((name: string) => { setAdding(false); setNotice(`Стажёр ${name} назначен вам.`); }, []);
  return <section className={styles.page}><header className={styles.heading}><h1>Назначенные стажёры</h1><p className={styles.muted}>Прогресс адаптации и вопросы, в которых нужна ваша помощь.</p></header>
    <div><Button onClick={() => { setNotice(''); setAdding(true); }}>Добавить стажёра</Button></div>
    {notice && query.data && <p role="status">{notice}</p>}
    <QueryFeedback query={query} />
    {query.data && (query.data.items.length ? <ul className={`${styles.list} ${styles.grid}`}>{query.data.items.map(intern => <li key={intern.id} className={styles.card}><h2><Link to={`/mentor/interns/${encodeURIComponent(intern.id)}`}>{intern.name}</Link></h2><p className={styles.muted}>{intern.team.name}</p><p>Прогресс: {intern.progress_percent}% · {intern.completed_steps} из {intern.total_steps} шагов</p><progress className={styles.progress} value={intern.progress_percent} max={100} aria-label={`Прогресс ${intern.name}`} /><p>Блокировки: {intern.blocked_steps} · Открытые вопросы: {intern.open_questions ?? 'нет данных'}</p><p className={styles.muted}>{intern.next_step_id ? <Link to={`/mentor/interns/${encodeURIComponent(intern.id)}`}>Посмотреть следующий шаг</Link> : 'Следующий шаг не назначен'}</p></li>)}</ul> : <EmptyState title="Стажёры пока не назначены" description="Здесь появятся назначенные вам стажёры." />)}
    {adding && <AssignInternDialog onClose={close} onAssigned={assigned} />}
  </section>;
}
export function MentorInternPlanPage() {
  const { id = '' } = useParams();
  const query = useMentorQuery(mentorKeys.plan(id), signal => mentorApi.plan(id, signal));
  const plan = query.data;
  const missing = query.error instanceof ClientError && query.error.status === 404;
  return <section className={styles.page}><Link to="/mentor/interns">← Назначенные стажёры</Link><h1>План стажёра</h1>
    {missing ? <InlineError message="План недоступен или ещё не подготовлен. Попробуйте загрузить план позже." onRetry={() => void query.refetch()} isRetrying={query.isFetching} /> : <QueryFeedback query={query} />}
    {plan && <><header className={styles.card}><h2>{plan.title}</h2><p>Прогресс: {plan.progress_percent}%</p><progress className={styles.progress} max={100} value={plan.progress_percent} aria-label="Прогресс плана" /><p className={styles.muted}>План доступен только для просмотра. Стажёр сам отмечает выполнение шагов.</p><p>Следующий шаг: {plan.steps.find(step => step.id === plan.next_step_id)?.title ?? 'Нет следующего шага'}</p></header>
      {plan.steps.length ? <ol className={styles.list}>{plan.steps.map(step => <li key={step.id} className={styles.card}><span className={styles.tag}>{stepLabels[step.status]}</span><h2>{step.title}</h2><p>{step.description}</p>{step.blocked_reason && <p>Причина блокировки: {step.blocked_reason}</p>}</li>)}</ol> : <EmptyState title="В плане пока нет шагов" description="План ещё не заполнен." />}</>}
  </section>;
}
