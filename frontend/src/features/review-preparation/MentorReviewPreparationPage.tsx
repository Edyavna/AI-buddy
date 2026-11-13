import { Link, useParams } from 'react-router-dom';
import { MarkdownContent } from '@/features/documents';
import { EmptyState } from '@/shared/ui';
import { useMentorQuery, QueryFeedback, dateLabel } from '@/features/mentor/common';
import { reviewPrepApi, reviewPrepKeys } from './api';
import styles from './ReviewPreparation.module.css';

const statusLabels = { passed: 'Успешно', failed: 'С ошибкой', skipped: 'Пропущено' };

export function MentorReviewPreparationPage() {
  const { id = '' } = useParams();
  const query = useMentorQuery(reviewPrepKeys.mentorItem(id), signal => reviewPrepApi.mentorItem(id, signal));
  const templateQuery = useMentorQuery(reviewPrepKeys.template, reviewPrepApi.template);
  const labels = new Map((templateQuery.data?.items ?? []).map(item => [item.key, item.label]));
  if (query.data === null) {
    return <section className={styles.page}><Link to="/mentor/review-preparations">← Подготовки к ревью</Link>
      <EmptyState title="Карточка недоступна" description="Подготовка не найдена или стажёр не назначен вам." /></section>;
  }
  const prep = query.data;
  return <section className={styles.page}>
    <Link to="/mentor/review-preparations">← Подготовки к ревью</Link>
    <QueryFeedback query={query} />
    {prep && <div className={styles.stack}>
      <div className={styles.card}>
        <div className={styles.row}>
          <span className={`${styles.tag} ${prep.status === 'submitted' ? styles.tagSuccess : styles.tagNeutral}`}>{prep.status === 'submitted' ? 'Отправлено на ревью' : 'Черновик'}</span>
          <span className={styles.muted}>Ревизия {prep.revision}{prep.submitted_at ? ` · отправлено ${dateLabel(prep.submitted_at)}` : ''}</span>
        </div>
        <h1>{prep.pr.title || `PR #${prep.pr.number}`}</h1>
        <p><a href={prep.pr.url} target="_blank" rel="noopener noreferrer">{prep.pr.url}</a></p>
        <p className={styles.muted}>{prep.pr.repo} · источник: {prep.pr.source === 'github' ? 'интеграция GitHub' : 'заполнено вручную'}
          {prep.pr.head_sha && <> · коммит {prep.pr.head_sha.slice(0, 7)}</>}
          {prep.pr.fetched_at && <> · синхронизировано {dateLabel(prep.pr.fetched_at)}</>}</p>
        {prep.pr.fetch_error && <p role="alert" className={styles.warnLine}>{prep.pr.fetch_error}</p>}
      </div>

      {prep.description && <div className={styles.card}><h2>Описание</h2><MarkdownContent content={prep.description} /></div>}

      <div className={styles.card}>
        <h2>Чек-лист</h2>
        <ul className={styles.checklist}>
          {Object.entries(prep.answers.items).map(([key, answer]) => <li key={key}>
            <span className={`${styles.tag} ${answer.done ? styles.tagSuccess : styles.tagNeutral}`}>{answer.done ? 'Готово' : 'Не сделано'}</span>
            <span>{labels.get(key) ?? key}</span>
            {answer.note && <span className={styles.muted}>{answer.note}</span>}
          </li>)}
          {Object.keys(prep.answers.items).length === 0 && <li className={styles.muted}>Чек-лист не заполнен.</li>}
        </ul>
      </div>

      <div className={styles.card}>
        <h2>Сведения</h2>
        <h3>Что сделано</h3>
        {prep.answers.summary ? <MarkdownContent content={prep.answers.summary} /> : <p className={styles.muted}>Не заполнено.</p>}
        <h3>Как проверено</h3>
        {prep.answers.verification ? <MarkdownContent content={prep.answers.verification} /> : <p className={styles.muted}>Не заполнено.</p>}
        {prep.answers.limitations && <><h3>Ограничения</h3><MarkdownContent content={prep.answers.limitations} /></>}
        {prep.answers.questions && <><h3>Вопросы ревьюеру</h3><MarkdownContent content={prep.answers.questions} /></>}
      </div>

      <div className={styles.card}>
        <h2>Результаты проверок</h2>
        {prep.checks.length === 0 && <p className={styles.muted}>Результатов пока нет.</p>}
        <ul className={styles.checks}>
          {prep.checks.map(check => <li key={check.id} className={styles.checkRow}>
            <span className={`${styles.tag} ${check.status === 'passed' ? styles.tagSuccess : check.status === 'failed' ? styles.tagDanger : styles.tagNeutral}`}>{statusLabels[check.status]}</span>
            <span>{check.name}</span>
            <span className={styles.sourceTag}>{check.source === 'ci' ? 'CI' : 'вручную'}</span>
            {check.detail && <span className={styles.muted}>{check.detail}</span>}
          </li>)}
        </ul>
        <p className={styles.muted}>Результаты «CI» получены из интеграции и привязаны к указанному коммиту; «вручную» указал стажёр.</p>
      </div>

      {prep.history.length > 0 && <div className={styles.card}>
        <h2>История отправок</h2>
        <ul>
          {prep.history.map(entry => <li key={entry.revision}>Ревизия {entry.revision} — отправлено {dateLabel(entry.submitted_at)}</li>)}
        </ul>
      </div>}
      <p className={styles.muted}>Итоговое принятие результата фиксируется подтверждением соответствующего чекпоинта в плане стажёра.</p>
    </div>}
  </section>;
}
