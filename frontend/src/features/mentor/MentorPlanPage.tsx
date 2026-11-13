import { useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { useMe } from '@/features/session';
import type { Step } from '@/features/onboarding';
import { MarkdownContent } from '@/features/documents';
import { Button, EmptyState, InlineError } from '@/shared/ui';
import { mentorApi, mentorKeys } from './api';
import { acceptMentorPlan, setMentorConfirmation } from './checkpoints';
import { useAction, useMentorQuery, QueryFeedback, dateLabel } from './common';
import styles from './Mentor.module.css';

const statuses = { not_started: 'Не начат', in_progress: 'В работе', blocked: 'Заблокирован', completed: 'Завершён' };
const policies = { intern: 'Подтверждает стажёр', mentor: 'Подтверждает наставник', both: 'Подтверждают стажёр и наставник' };
export function safeResourceUrl(value: string | null): string | null {
  if (!value) return null;
  try { const url = new URL(value); return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password ? url.href : null; }
  catch { return null; }
}
function Checkpoint({ step, busy, pending, onChange }: { step: Step; busy: boolean; pending: boolean; onChange: (confirmed: boolean) => void }) {
  return <li className={styles.card} data-step-id={step.id}>
    <div className={styles.row}><span className={styles.tag}>{statuses[step.status]}</span>{step.period && <span className={styles.muted}>Период: {step.period}</span>}</div>
    <h2>{step.title}</h2>
    {step.description && step.description !== step.instructions && step.description !== step.title && <p>{step.description}</p>}
    {step.instructions && step.instructions !== step.title && <section aria-label={`Инструкция: ${step.title}`}><h3>Инструкция</h3><MarkdownContent content={step.instructions} /></section>}
    {step.resources.length > 0 && <section aria-label={`Материалы: ${step.title}`}><h3>Материалы</h3><ul>{step.resources.map((resource, index) => {
      const url = safeResourceUrl(resource.url);
      return <li key={`${resource.title}-${index}`}>{url ? <a href={url} target="_blank" rel="noopener noreferrer">{resource.title}</a> : <span>{resource.title}</span>}<p className={styles.muted}>{resource.note ?? (!url ? 'Уточнить у наставника' : '')}</p></li>;
    })}</ul></section>}
    <section aria-label={`Подтверждения: ${step.title}`} className={styles.confirmations}>
      <h3>{policies[step.completion_policy]}</h3>
      {(['intern', 'mentor'] as const).filter(role => step.completion_policy === 'both' || role === step.completion_policy).map(role => {
        const mark = step.confirmations.find(c => c.role === role);
        return <p key={role}>{role === 'intern' ? 'Стажёр' : 'Наставник'}: {mark ? <>подтверждено <time dateTime={mark.confirmed_at}>{dateLabel(mark.confirmed_at)}</time></> : 'ожидается подтверждение'}</p>;
      })}
      {step.awaiting_role && <p>{step.awaiting_role === 'intern' ? 'Ожидаем подтверждение стажёра' : 'Ожидаем подтверждение наставника'}</p>}
    </section>
    {step.blocked_reason && <p>Причина блокировки: {step.blocked_reason}</p>}
    {step.completion_policy !== 'intern' && step.allowed_actions.length > 0 && <div className={styles.row}>{step.allowed_actions.map(action => <Button key={action} variant={action === 'confirm' ? 'primary' : 'secondary'} disabled={busy} isLoading={pending} aria-label={action === 'confirm' ? 'Подтвердить свою часть' : 'Отозвать свою отметку'} onClick={() => onChange(action === 'confirm')}>{action === 'confirm' ? 'Подтвердить свою часть' : 'Отозвать свою отметку'}</Button>)}</div>}
  </li>;
}
export function MentorInternPlanPage() {
  const { id = '' } = useParams();
  return <MentorPlan key={id} id={id} />;
}
function MentorPlan({ id }: { id: string }) {
  const client = useQueryClient(), me = useMe(), action = useAction();
  const [pendingStep, setPendingStep] = useState('');
  const query = useMentorQuery(mentorKeys.plan(id), async signal => acceptMentorPlan(client, id, await mentorApi.plan(id, signal), signal));
  const summary = useMentorQuery(mentorKeys.interns, mentorApi.interns);
  const intern = summary.data?.items.find(i => i.id === id), plan = query.data;
  function change(step: Step, confirmed: boolean) {
    if (!me.data || step.completion_policy === 'intern' || !step.allowed_actions.includes(confirmed ? 'confirm' : 'revoke')) return;
    const mentorId = me.data.id;
    void action.run(async signal => { setPendingStep(step.id); await setMentorConfirmation(client, id, mentorId, step.id, confirmed, step.revision, signal); });
  }
  const missing = query.error instanceof ClientError && query.error.status === 404;
  return <section className={styles.page}>
    <Link to="/mentor/interns">← Назначенные стажёры</Link><h1>План стажёра</h1>
    {missing ? <InlineError message="План недоступен или ещё не подготовлен. Попробуйте загрузить план позже." onRetry={() => void query.refetch()} isRetrying={query.isFetching} /> : <QueryFeedback query={query} />}
    {!query.isPending && !query.isError && !plan && <InlineError message="План недоступен. Обновите данные перед продолжением." onRetry={() => void query.refetch()} />}
    {plan && <><header className={styles.card}><h2>{plan.title}</h2>{intern && <p>{intern.name} · {intern.team.name}</p>}<p>Прогресс: {plan.progress_percent}%</p><progress className={styles.progress} max={100} value={plan.progress_percent} aria-label="Прогресс плана" />
      {intern && <p>Завершено {intern.completed_steps} из {intern.total_steps} шагов · Блокировки: {intern.blocked_steps}</p>}
      <p className={styles.muted}>Подтверждайте свою часть там, где это разрешено. Отметка стажёра сохраняется независимо.</p>
      <p>Следующий шаг: {plan.steps.find(s => s.id === plan.next_step_id)?.title ?? 'Нет следующего шага'}</p>
      <QueryFeedback query={summary} /></header>
      {action.error && <InlineError message={action.error} />}
      {plan.steps.length ? <ol className={styles.list}>{plan.steps.map(step => <Checkpoint key={step.id} step={step} busy={action.busy} pending={action.busy && pendingStep === step.id} onChange={confirmed => change(step, confirmed)} />)}</ol> : <EmptyState title="В плане пока нет шагов" description="План ещё не заполнен." />}
    </>}
  </section>;
}
