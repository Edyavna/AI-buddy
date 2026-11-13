import { useEffect, useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Button, InlineError, StatusBadge, TextField } from '@/shared/ui';
import { MarkdownContent } from '@/features/documents';
import { onboardingApi } from '@/features/onboarding';
import { useAction, useMentorQuery, QueryFeedback, dateLabel } from '@/features/mentor/common';
import { reviewPrepApi, reviewPrepKeys } from './api';
import type { ReviewCheck, ReviewPreparation, ReviewTemplateItem } from './schemas';
import styles from './ReviewPreparation.module.css';

const statusLabels = { passed: 'Успешно', failed: 'С ошибкой', skipped: 'Пропущено' };

type DraftState = {
  description: string;
  summary: string;
  verification: string;
  limitations: string;
  questions: string;
  items: Record<string, { done: boolean; note: string }>;
  checks: { name: string; status: 'passed' | 'failed' | 'skipped'; detail: string }[];
};

function draftOf(prep: ReviewPreparation, template: ReviewTemplateItem[]): DraftState {
  const items: DraftState['items'] = {};
  for (const item of template) {
    items[item.key] = prep.answers.items[item.key] ?? { done: false, note: '' };
  }
  return {
    description: prep.description,
    summary: prep.answers.summary,
    verification: prep.answers.verification,
    limitations: prep.answers.limitations,
    questions: prep.answers.questions,
    items,
    checks: prep.checks.filter(check => check.source === 'intern').map(check => ({ name: check.name, status: check.status, detail: check.detail })),
  };
}

function CreateForm({ onCreated }: { onCreated: (prep: ReviewPreparation) => void }) {
  const action = useAction();
  const [url, setUrl] = useState('');
  const [manual, setManual] = useState(false);
  const [stepId, setStepId] = useState('');
  const planQuery = useMentorQuery(['review-preparation', 'plan'], async signal => {
    const result = await onboardingApi.getMyPlan(signal);
    return result.data;
  });
  const steps = planQuery.data?.steps ?? [];
  const invalid = url.trim() !== '' && !/^https?:\/\/github\.com\/[\w.-]+\/[\w.-]+\/pull\/\d+\/?$/.test(url.trim());
  const submit = () => action.run(async signal => {
    const prep = await reviewPrepApi.create(url.trim(), manual, signal);
    onCreated(prep);
  });
  return <div className={styles.card}>
    <h2>Новая подготовка к ревью</h2>
    <p className={styles.muted}>Прикрепите ссылку на pull request — поддерживается адрес вида github.com/владелец/репозиторий/pull/номер.</p>
    <TextField label="Ссылка на PR" value={url} onChange={event => setUrl(event.target.value)} placeholder="https://github.com/владелец/репозиторий/pull/1" {...(invalid ? { errorText: 'Поддерживается только github.com/владелец/репозиторий/pull/номер' } : {})} fullWidth />
    {steps.length > 0 && <label className={styles.field}>Связанный шаг плана
      <select value={stepId} onChange={event => setStepId(event.target.value)} className={styles.select}>
        <option value="">Без привязки</option>
        {steps.map(step => <option key={step.id} value={step.id}>{step.title}</option>)}
      </select>
    </label>}
    <label className={styles.checkLine}>
      <input type="checkbox" checked={manual} onChange={event => setManual(event.target.checked)} />
      Заполнить сведения вручную без интеграции GitHub
    </label>
    <Button disabled={url.trim() === '' || invalid} isLoading={action.busy} onClick={() => void submit()}>Создать черновик</Button>
    {action.error && <InlineError message={action.error} />}
  </div>;
}

export function ReviewPreparationPage() {
  const queryClient = useQueryClient();
  const templateQuery = useMentorQuery(reviewPrepKeys.template, reviewPrepApi.template);
  const latestQuery = useMentorQuery(reviewPrepKeys.latest, reviewPrepApi.latest);
  const prep = latestQuery.data?.item ?? null;
  const template = templateQuery.data;
  return <section className={styles.page}>
    <header className={styles.heading}>
      <h1>Подготовка к ревью</h1>
      <p className={styles.muted}>Соберите сведения о PR по чек-листу команды и отправьте их наставнику одной карточкой.</p>
    </header>
    <QueryFeedback query={templateQuery} />
    <QueryFeedback query={latestQuery} />
    {template && (prep
      ? <PreparationEditor key={prep.id} prep={prep} template={template} onChange={next => queryClient.setQueryData(reviewPrepKeys.latest, { item: next })} />
      : <CreateForm onCreated={created => queryClient.setQueryData(reviewPrepKeys.latest, { item: created })} />)}
  </section>;
}

function PreparationEditor({ prep, template, onChange }: { prep: ReviewPreparation; template: { items: ReviewTemplateItem[] }; onChange: (prep: ReviewPreparation) => void }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<DraftState>(() => draftOf(prep, template.items));
  const [dirty, setDirty] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [notice, setNotice] = useState('');
  const [help, setHelp] = useState<{ summary: string; missing: string[] } | null>(null);
  const saveAction = useAction();
  const submitAction = useAction();
  const assistantAction = useAction();
  const refreshAction = useAction();

  useEffect(() => { setDraft(draftOf(prep, template.items)); setDirty(false); setConfirming(false); }, [prep.id, prep.revision, prep.updated_at, template.items]);
  const patch = (change: Partial<DraftState>) => { setDraft(current => ({ ...current, ...change })); setDirty(true); setConfirming(false); };

  const ciChecks = useMemo(() => prep.checks.filter(check => check.source === 'ci'), [prep.checks]);

  const buildUpsert = (revision: number) => ({
    revision,
    description: draft.description,
    answers: {
      summary: draft.summary,
      verification: draft.verification,
      limitations: draft.limitations,
      questions: draft.questions,
      items: draft.items,
    },
    checks: draft.checks,
  });

  const save = async (signal: AbortSignal) => {
    const saved = await reviewPrepApi.update(prep.id, buildUpsert(prep.revision), signal);
    onChange(saved);
    return saved;
  };
  const submit = () => submitAction.run(async signal => {
    // Потерянный ответ сверяется с сервером: сначала сохраняем, потом отправляем сохранённую ревизию.
    const saved = dirty ? await save(signal) : prep;
    const sent = await reviewPrepApi.submit(prep.id, saved.revision, signal);
    onChange(sent);
    setNotice(sent.revision > saved.revision ? `Отправлено на ревью, ревизия ${sent.revision}. Наставник увидит новую версию.` : 'Подготовка отправлена на ревью.');
    queryClient.invalidateQueries({ queryKey: reviewPrepKeys.mentorList });
  });
  const askAssistant = () => assistantAction.run(async signal => {
    setHelp(await reviewPrepApi.assistant(prep.id, signal));
  });
  const refresh = () => refreshAction.run(async signal => {
    const result = await reviewPrepApi.refresh(prep.id, signal);
    onChange(result.preparation);
    setNotice(result.changed
      ? `PR обновился: сведения и результаты CI привязаны к коммиту ${result.preparation.pr.head_sha.slice(0, 7)}. Проверьте, что резюме актуально.`
      : 'Сведения о PR актуальны, новых коммитов нет.');
  });

  const busy = saveAction.busy || submitAction.busy;
  return <div className={styles.stack}>
    <div className={styles.card}>
      <div className={styles.row}>
        <StatusBadge status={prep.status === 'draft' ? 'draft' : 'completed'} label={prep.status === 'draft' ? 'Черновик' : 'Отправлено на ревью'} />
        <span className={styles.muted}>Ревизия {prep.revision}{prep.submitted_at ? ` · отправлено ${dateLabel(prep.submitted_at)}` : ''}</span>
      </div>
      <h2>{prep.pr.title || `PR #${prep.pr.number}`}</h2>
      <p>
        <a href={prep.pr.url} target="_blank" rel="noopener noreferrer">{prep.pr.repo}#{prep.pr.number}</a>
        <span className={styles.muted}> · источник: {prep.pr.source === 'github' ? 'интеграция GitHub' : 'заполнено вручную'}</span>
        {prep.pr.head_sha && <span className={styles.muted}> · коммит {prep.pr.head_sha.slice(0, 7)}</span>}
        {prep.pr.fetched_at && <span className={styles.muted}> · синхронизировано {dateLabel(prep.pr.fetched_at)}</span>}
      </p>
      {prep.pr.fetch_error && <InlineError message={prep.pr.fetch_error} {...(prep.pr.source === 'github' ? { onRetry: () => void refresh(), isRetrying: refreshAction.busy } : {})} />}
      {notice && <p role="status" className={styles.notice}>{notice}</p>}
      {prep.pr.source === 'github' && <Button variant="ghost" size="sm" isLoading={refreshAction.busy} onClick={() => void refresh()}>Проверить обновления PR</Button>}
    </div>

    <div className={styles.card}>
      <h2>Чек-лист команды</h2>
      <ul className={styles.checklist}>
        {template.items.map(item => <li key={item.key}>
          <label className={styles.checkLine}>
            <input type="checkbox" checked={draft.items[item.key]?.done ?? false} onChange={event => patch({ items: { ...draft.items, [item.key]: { ...draft.items[item.key], done: event.target.checked, note: draft.items[item.key]?.note ?? '' } } })} />
            <span>{item.label}{item.required && <span className={styles.required} aria-label="обязательный пункт"> *</span>}</span>
          </label>
          <input className={styles.noteInput} aria-label={`Комментарий: ${item.label}`} placeholder="Комментарий" value={draft.items[item.key]?.note ?? ''} onChange={event => patch({ items: { ...draft.items, [item.key]: { done: draft.items[item.key]?.done ?? false, note: event.target.value } } })} />
        </li>)}
      </ul>
    </div>

    <div className={styles.card}>
      <h2>Что сделано и как проверено</h2>
      <label className={styles.field}>Резюме (что сделано)
        <textarea className={styles.area} rows={4} value={draft.summary} onChange={event => patch({ summary: event.target.value })} />
      </label>
      <label className={styles.field}>Как проверено
        <textarea className={styles.area} rows={3} value={draft.verification} onChange={event => patch({ verification: event.target.value })} />
      </label>
      <label className={styles.field}>Ограничения и известные проблемы
        <textarea className={styles.area} rows={3} value={draft.limitations} onChange={event => patch({ limitations: event.target.value })} />
      </label>
      <label className={styles.field}>Вопросы ревьюеру
        <textarea className={styles.area} rows={3} value={draft.questions} onChange={event => patch({ questions: event.target.value })} />
      </label>
      <label className={styles.field}>Описание для карточки (Markdown)
        <textarea className={styles.area} rows={5} value={draft.description} onChange={event => patch({ description: event.target.value })} />
      </label>
    </div>

    <div className={styles.card}>
      <h2>Результаты проверок</h2>
      {ciChecks.length > 0 && <ul className={styles.checks}>
        {ciChecks.map(check => <CheckRow key={check.id} check={check} />)}
      </ul>}
      {ciChecks.length === 0 && prep.pr.source === 'github' && <p className={styles.muted}>CI не обнаружен на последнем коммите — укажите проверки вручную ниже.</p>}
      <ul className={styles.checks}>
        {draft.checks.map((check, index) => <li key={index} className={styles.internCheck}>
          <input aria-label="Название проверки" placeholder="Например: go test ./..." value={check.name} onChange={event => patch({ checks: draft.checks.map((item, i) => i === index ? { ...item, name: event.target.value } : item) })} />
          <select aria-label="Статус проверки" value={check.status} onChange={event => patch({ checks: draft.checks.map((item, i) => i === index ? { ...item, status: event.target.value as 'passed' | 'failed' | 'skipped' } : item) })}>
            {(['passed', 'failed', 'skipped'] as const).map(status => <option key={status} value={status}>{statusLabels[status]}</option>)}
          </select>
          <input aria-label="Детали проверки" placeholder="Детали" value={check.detail} onChange={event => patch({ checks: draft.checks.map((item, i) => i === index ? { ...item, detail: event.target.value } : item) })} />
          <Button variant="ghost" size="sm" aria-label="Удалить проверку" onClick={() => patch({ checks: draft.checks.filter((_, i) => i !== index) })}>Удалить</Button>
        </li>)}
      </ul>
      <Button variant="ghost" size="sm" onClick={() => patch({ checks: [...draft.checks, { name: '', status: 'passed', detail: '' }] })}>Добавить проверку</Button>
      <p className={styles.muted}>Результаты со значком «CI» получены из интеграции GitHub и привязаны к коммиту; ручные результаты указываете вы.</p>
    </div>

    {help && <div className={styles.card} role="status">
      <h2>Помощь с описанием</h2>
      {help.missing.length > 0 && <p>Пропущенные пункты: {help.missing.join('; ')}.</p>}
      <MarkdownContent content={help.summary} />
      <Button variant="ghost" size="sm" onClick={() => { patch({ description: help.summary }); setHelp(null); }}>Вставить в описание</Button>
    </div>}

    <div className={styles.actions}>
      <Button variant="secondary" isLoading={saveAction.busy} disabled={busy} onClick={() => void saveAction.run(async signal => { await save(signal); setNotice('Черновик сохранён.'); })}>
        {prep.status === 'draft' ? 'Сохранить черновик' : 'Сохранить обновление'}
      </Button>
      <Button variant="ghost" isLoading={assistantAction.busy} disabled={busy} onClick={() => void askAssistant()}>Помощь с описанием</Button>
      {confirming
        ? <Button variant="primary" isLoading={submitAction.busy} onClick={() => { setConfirming(false); void submit(); }}>Подтвердить отправку на ревью</Button>
        : <Button variant="primary" disabled={busy} onClick={() => setConfirming(true)}>{prep.status === 'draft' ? 'Отправить на ревью' : 'Отправить обновление'}</Button>}
      {confirming && <Button variant="ghost" onClick={() => setConfirming(false)}>Отмена</Button>}
    </div>
    {(saveAction.error || submitAction.error || assistantAction.error) && <InlineError message={saveAction.error || submitAction.error || assistantAction.error} />}
    {prep.status === 'submitted' && <p className={styles.muted}>Итоговое принятие результата наставник фиксирует в подтверждении чекпоинта плана.</p>}
  </div>;
}

function CheckRow({ check }: { check: ReviewCheck }) {
  return <li className={styles.checkRow}>
    <span className={`${styles.tag} ${check.status === 'passed' ? styles.tagSuccess : check.status === 'failed' ? styles.tagDanger : styles.tagNeutral}`}>{statusLabels[check.status]}</span>
    <span>{check.name}</span>
    <span className={styles.sourceTag}>{check.source === 'ci' ? 'CI' : 'вручную'}</span>
    {check.detail && <span className={styles.muted}>{check.detail}</span>}
  </li>;
}
