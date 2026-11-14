import { useRef, useState } from 'react';
import { z } from 'zod';
import { isoDate, LoadState, localDate, styles, useWorkspace } from './shared';

const step = z.object({ key: z.string(), title: z.string(), description: z.string() });
const evidence = z.object({ kind: z.string(), id: z.string() });
const problem = z.object({
  id: z.string(), kind: z.string(), direction: z.string(), title: z.string(), description: z.string(),
  status: z.string(), owner_id: z.string().nullable(), due_at: z.string().nullable(), resolution: z.string(),
  document_id: z.string().nullable(), template_version: z.number().nullable(), merged_into: z.string().nullable(),
  revision: z.number(), evidence: z.array(evidence), history: z.array(z.object({
    revision: z.number(), actor_id: z.string(), changed_at: z.string(), snapshot: z.object({ status: z.string(), title: z.string() }),
  })),
});
const workspace = z.object({
  teams: z.array(z.object({ id: z.string(), name: z.string() })),
  owners: z.array(z.object({ id: z.string(), name: z.string(), role: z.string() })),
  plan_count: z.number(),
  template: z.object({ version: z.number(), steps: z.array(step) }).nullable(),
  problems: z.array(problem),
  summaries: z.array(z.object({
    kind: z.string(), direction: z.string(), topic: z.string(), events: z.number(), interns: z.number(),
    average_hours: z.number().nullable(), earlier_events: z.number(), later_events: z.number(), source_ids: z.array(z.string()),
  })),
});
type Workspace = z.infer<typeof workspace>;
type Problem = z.infer<typeof problem>;
type Send = (body: unknown) => Promise<boolean>;
const kinds = { access: 'Задержки доступов', blocked: 'Блокеры', knowledge: 'Пробелы знаний' };
const statuses = { open: 'Открыта', in_progress: 'В работе', resolved: 'Решена', merged: 'Объединена' };
const kindLabel = (kind: string) => kinds[kind as keyof typeof kinds] ?? kind;
const statusLabel = (status: string) => statuses[status as keyof typeof statuses] ?? status;
const directionLabel = (direction: string) => direction === 'hr' ? 'HR' : 'Наставник';
function Options({ values }: { values: Record<string, string> }) {
  return <>{Object.entries(values).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</>;
}
function ProblemForm({ item, data, team, send, pending, discard }: {
  item: Problem; data: Workspace; team: string; send: Send; pending: boolean; discard: () => void;
}) {
  const [direction, setDirection] = useState(item.direction);
  const [kind, setKind] = useState(item.kind);
  const [selectedEvidence, setEvidence] = useState(item.evidence.map((source) => source.id).join('\n'));
  return <form className={styles.form} onSubmit={(event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const merge = data.problems.find((row) => row.id === form.get('merge_from'));
    void send({
      action: 'save-problem', id: item.id, team_id: team, revision: item.revision, kind, direction,
      title: form.get('title'), description: form.get('description'), status: form.get('status'), owner_id: form.get('owner_id') || null,
      due_at: isoDate(String(form.get('due_at') ?? '')), resolution: form.get('resolution'), document_id: form.get('document_id') || null,
      template_version: form.get('template_version') ? Number(form.get('template_version')) : null,
      evidence: selectedEvidence.split(/\s+/).filter(Boolean).map((id) => ({ kind, id })),
      ...(merge && { merge_from: merge.id, merge_revision: merge.revision }),
    }).then((ok) => { if (ok) discard(); });
  }}>
    <label>Тип<select value={kind} disabled={item.revision > 0} onChange={(e) => { setKind(e.target.value); if (e.target.value !== 'knowledge') setDirection('mentor'); }}><Options values={kinds} /></select></label>
    <label>Направление<select value={direction} disabled={item.revision > 0 || kind !== 'knowledge'} onChange={(e) => { setDirection(e.target.value); if (e.target.value === 'hr') setEvidence(''); }}><Options values={{ mentor: 'Наставник', hr: 'HR' }} /></select></label>
    <label>Название<input name="title" defaultValue={item.title} maxLength={200} required /></label>
    <label>Описание<textarea name="description" defaultValue={item.description} maxLength={4000} required /></label>
    <label>Состояние<select name="status" defaultValue={item.status}><Options values={{ open: 'Открыта', in_progress: 'В работе', resolved: 'Решена' }} /></select></label>
    <label>Владелец<select name="owner_id" defaultValue={item.owner_id ?? ''} key={direction}><option value="">Не назначен</option>{data.owners.filter((owner) => owner.role === direction).map((owner) => <option key={owner.id} value={owner.id}>{owner.name}</option>)}</select></label>
    <label>Срок<input type="datetime-local" name="due_at" defaultValue={item.due_at ? localDate(item.due_at) : ''} /></label>
    {direction === 'mentor' && <label>Идентификаторы исходных записей<textarea value={selectedEvidence} onChange={(e) => setEvidence(e.target.value)} /></label>}
    <label>Объединить с проблемой<select name="merge_from"><option value="">Не объединять</option>{data.problems.filter((row) => row.id !== item.id && row.direction === direction && row.kind === kind && row.status !== 'merged').map((row) => <option key={row.id} value={row.id}>{row.title}</option>)}</select></label>
    <label>Решение<textarea name="resolution" defaultValue={item.resolution} maxLength={4000} /></label>
    <label>Идентификатор опубликованной инструкции<input name="document_id" defaultValue={item.document_id ?? ''} /></label>
    <label>Версия опубликованного шаблона<input name="template_version" type="number" min="1" defaultValue={item.template_version ?? ''} /></label>
    <div className={styles.actions}><button disabled={pending}>Сохранить проблему</button><button type="button" onClick={discard}>Отмена</button></div>
  </form>;
}
function TemplateEditor({ data, team, send, pending }: { data: Workspace; team: string; send: Send; pending: boolean }) {
  const [draft, setDraft] = useState(data.template?.steps ?? [{ key: '', title: '', description: '' }]);
  const [preview, setPreview] = useState(false);
  const [existing, setExisting] = useState(false);
  const operation = useRef<string | undefined>(undefined);
  return <section className={styles.card}>
    <h2>Шаблон адаптации</h2>
    <p>Текущая версия {data.template?.version ?? 0}</p>
    <form className={styles.form} onSubmit={(e) => { e.preventDefault(); operation.current = crypto.randomUUID(); setPreview(true); }}>
      {draft.map((row, index) => <fieldset key={index} className={styles.form}>
        <legend>Чекпоинт</legend>
        {(['key', 'title', 'description'] as const).map((field) => <label key={field}>{{ key: 'Постоянный ключ', title: 'Название', description: 'Описание' }[field]}
          <input required maxLength={{ key: 80, title: 200, description: 4000 }[field]} value={row[field]} onChange={(e) => {
            setPreview(false);
            setDraft(draft.map((entry, i) => i === index ? { ...entry, [field]: e.target.value } : entry));
          }} />
        </label>)}
        <button type="button" disabled={draft.length === 1} onClick={() => { setPreview(false); setDraft(draft.filter((_, i) => i !== index)); }}>Убрать из шаблона</button>
      </fieldset>)}
      <label><input type="checkbox" checked={existing} onChange={(e) => { setPreview(false); setExisting(e.target.checked); }} />Применить к существующим планам</label>
      <div className={styles.actions}><button type="button" disabled={draft.length >= 50} onClick={() => { setPreview(false); setDraft([...draft, { key: '', title: '', description: '' }]); }}>Добавить чекпоинт</button><button>Предпросмотр публикации</button></div>
    </form>
    {preview && <section aria-label="Предпросмотр шаблона">
      <p>Версия {(data.template?.version ?? 0) + 1} станет основой для новых планов. {existing && `Будет обновлено планов: ${data.plan_count}. Отметки выполнения сохранятся.`}</p>
      <ol>{draft.map((row, i) => <li key={i}><strong>{row.title}</strong> — {row.description}</li>)}</ol>
      <button disabled={pending} onClick={() => void send({
        action: 'publish-template', id: operation.current, team_id: team, revision: data.template?.version ?? 0, steps: draft, existing_plans: existing,
      }).then((ok) => { if (ok) setPreview(false); })}>Опубликовать шаблон</button>
    </section>}
  </section>;
}

function newProblem(prefill?: Workspace['summaries'][number]): Problem {
  return { id: crypto.randomUUID(), kind: prefill?.kind ?? 'access', direction: prefill?.direction ?? 'mentor',
    title: prefill ? kindLabel(prefill.kind) + ' · ' + prefill.topic : '', description: '', owner_id: null, due_at: null,
    resolution: '', status: 'open', document_id: null, template_version: null, merged_into: null, revision: 0, history: [],
    evidence: prefill?.source_ids.map((id) => ({ id, kind: prefill.kind })) ?? [],
  };
}
export function CuratorPage() {
  const [team, setTeam] = useState('');
  const [filters, setFilters] = useState<Record<string, string | undefined>>({});
  const [editing, setEditing] = useState<Problem>();
  const work = useWorkspace('/curator', workspace, { team_id: team || undefined, ...filters });
  return <section className={styles.page}>
    <h1>Системные проблемы адаптации</h1>
    <LoadState loading={work.loading} error={work.error} retry={() => void work.load()}>
      {work.message && <p role="alert">{work.message}</p>}
      {!work.data?.teams.length ? <p>У вас нет назначенных команд куратора.</p> : <>
        <label>Команда <select value={team} onChange={(e) => { setEditing(undefined); setTeam(e.target.value); }}><option value="">Выберите команду</option>{work.data.teams.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
        {team && <>
          <form className={styles.form} onSubmit={(e) => {
            e.preventDefault();
            const form = new FormData(e.currentTarget);
            setFilters({ from: isoDate(String(form.get('from'))) ?? undefined, to: isoDate(String(form.get('to'))) ?? undefined, kind: String(form.get('kind')), direction: String(form.get('direction')), status: String(form.get('status')) });
          }}>
            <div className={styles.grid}>
              <label>Начало периода<input type="datetime-local" name="from" defaultValue={filters.from ? localDate(filters.from) : ''} /></label>
              <label>Конец периода<input type="datetime-local" name="to" defaultValue={filters.to ? localDate(filters.to) : ''} /></label>
              <label>Тип<select name="kind" defaultValue={filters.kind}><option value="">Все</option><Options values={kinds} /></select></label>
              <label>Направление<select name="direction" defaultValue={filters.direction}><option value="">Все</option><Options values={{ mentor: 'Наставник', hr: 'HR' }} /></select></label>
              <label>Состояние<select name="status" defaultValue={filters.status}><option value="">Все</option><Options values={statuses} /></select></label>
            </div>
            <button>Применить фильтры</button>
          </form>
          <h2>Повторяющиеся сигналы</h2>
          {!work.data.summaries.length && <p>За выбранный период повторяющихся сигналов нет.</p>}
          {work.data.summaries.map((signal) => <article key={JSON.stringify([signal.kind, signal.direction, signal.topic])} className={styles.card}>
            <h3>{kindLabel(signal.kind)} · {signal.topic}</h3>
            <p>{directionLabel(signal.direction)} · Стажёров {signal.interns} · Событий {signal.events}</p>
            <p>Ожидание {signal.average_hours === null ? 'нет данных' : `${signal.average_hours} ч`} · Динамика {signal.earlier_events} → {signal.later_events}</p>
            {signal.source_ids.length > 0 && <details><summary>Исходные записи</summary><ul>{signal.source_ids.map((id) => <li key={id}>{id}</li>)}</ul></details>}
            <button onClick={() => setEditing(newProblem(signal))}>Создать проблему по сигналу</button>
          </article>)}
          <h2>Проблемы</h2>
          <button onClick={() => setEditing(newProblem())}>Добавить проблему</button>
          {editing && <ProblemForm key={editing.id + ':' + editing.revision} item={editing} data={work.data} team={team} send={work.send} pending={work.pending} discard={() => setEditing(undefined)} />}
          {!work.data.problems.length && <p>Проблем пока нет.</p>}
          {work.data.problems.map((item) => <article key={item.id} className={styles.card}>
            <h3>{item.title}</h3><p>{statusLabel(item.status)} · {directionLabel(item.direction)} · {item.owner_id ?? 'Без владельца'}</p>
            <p>{item.description}</p>{item.resolution && <p>{item.resolution}</p>}
            <details><summary>Доказательства и история</summary><ul>{item.evidence.map((row) => <li key={row.kind + row.id}>{row.id}</li>)}
              {item.history.map((entry) => <li key={entry.revision}>{localDate(entry.changed_at)} · {entry.actor_id} · {statusLabel(entry.snapshot.status)}</li>)}</ul></details>
            {item.status !== 'merged' && <button onClick={() => setEditing(item)}>Редактировать проблему</button>}
          </article>)}
          <TemplateEditor key={team + ':' + (work.data.template?.version ?? 0)} data={work.data} team={team} send={work.send} pending={work.pending} />
        </>}
      </>}
    </LoadState>
  </section>;
}
