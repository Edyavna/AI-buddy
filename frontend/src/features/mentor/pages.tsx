import { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, EmptyState } from '@/shared/ui';
import { AssignInternDialog } from './AssignInternDialog';
import { mentorApi, mentorKeys } from './api';
import { useMentorQuery, QueryFeedback } from './common';
import styles from './Mentor.module.css';

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
export { MentorInternPlanPage } from './MentorPlanPage';
