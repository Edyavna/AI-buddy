import { Link, useParams } from 'react-router-dom';
import { EmptyState } from '@/shared/ui';
import { mentorApi, mentorKeys } from './api';
import { useMentorQuery, QueryFeedback } from './common';
import styles from './Mentor.module.css';

const stepLabels = { not_started: 'Не начат', in_progress: 'В работе', blocked: 'Заблокирован', completed: 'Завершён' };
export function MentorInternsPage() {
  const query = useMentorQuery(mentorKeys.interns, mentorApi.interns);
  return <section className={styles.page}><header className={styles.heading}><h1>Назначенные стажёры</h1><p className={styles.muted}>Прогресс адаптации и вопросы, в которых нужна ваша помощь.</p></header><QueryFeedback query={query} />
    {query.data && (query.data.items.length ? <ul className={`${styles.list} ${styles.grid}`}>{query.data.items.map(intern => <li key={intern.id} className={styles.card}><h2><Link to={`/mentor/interns/${encodeURIComponent(intern.id)}`}>{intern.name}</Link></h2><p className={styles.muted}>{intern.team.name}</p><p>Прогресс: {intern.progress_percent}% · {intern.completed_steps} из {intern.total_steps} шагов</p><progress className={styles.progress} value={intern.progress_percent} max={100} aria-label={`Прогресс ${intern.name}`} /><p>Блокировки: {intern.blocked_steps} · Открытые вопросы: {intern.open_questions}</p><p className={styles.muted}>{intern.next_step_id ? <Link to={`/mentor/interns/${encodeURIComponent(intern.id)}`}>Посмотреть следующий шаг</Link> : 'Следующий шаг не назначен'}</p></li>)}</ul> : <EmptyState title="Стажёры пока не назначены" description="Здесь появятся назначенные вам стажёры." />)}
  </section>;
}
export function MentorInternPlanPage() {
  const { id = '' } = useParams();
  const query = useMentorQuery(mentorKeys.plan(id), signal => mentorApi.plan(id, signal));
  const plan = query.data;
  return <section className={styles.page}><Link to="/mentor/interns">← Назначенные стажёры</Link><h1>План стажёра</h1><QueryFeedback query={query} />
    {plan && <><header className={styles.card}><h2>{plan.title}</h2><p>Прогресс: {plan.progress_percent}%</p><progress className={styles.progress} max={100} value={plan.progress_percent} aria-label="Прогресс плана" /><p className={styles.muted}>План доступен только для просмотра. Стажёр сам отмечает выполнение шагов.</p><p>Следующий шаг: {plan.steps.find(step => step.id === plan.next_step_id)?.title ?? 'Нет следующего шага'}</p></header>
      {plan.steps.length ? <ol className={styles.list}>{plan.steps.map(step => <li key={step.id} className={styles.card}><span className={styles.tag}>{stepLabels[step.status]}</span><h2>{step.title}</h2><p>{step.description}</p>{step.blocked_reason && <p>Причина блокировки: {step.blocked_reason}</p>}</li>)}</ol> : <EmptyState title="В плане пока нет шагов" description="План ещё не заполнен." />}</>}
  </section>;
}
