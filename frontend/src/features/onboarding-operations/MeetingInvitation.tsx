import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { z } from 'zod';
import { apiRequest, ClientError } from '@/shared/api';
import { styles } from './shared';

export const agendaEntry = z.object({
  id: z.string().optional(), kind: z.enum(['checkpoint', 'question', 'agreement']),
  text: z.string(), step_id: z.string().nullable(), state: z.string(),
  reason: z.string().nullable().optional(), included: z.boolean().default(true),
});
export type AgendaEntry = z.infer<typeof agendaEntry>;

const proposal = z.object({
  id: z.string().uuid(), reason: z.enum(['explicit', 'priorities', 'feedback', 'blocker', 'overdue']),
  mentor_id: z.string(), existing_id: z.string(), date: z.string(), time: z.string(),
  timezone: z.string(), title: z.string(), topics: z.array(z.string()), missing: z.array(z.string()),
});
export type Proposal = z.infer<typeof proposal>;
export const proposalLocation = z.object({ owner: z.string(), draft: proposal });
const suggestion = z.object({ proposal: proposal.nullable() });
const reasons = {
  explicit: 'Можно подготовить встречу с наставником.',
  blocker: 'Предлагаю обсудить подтверждённый блокер на 1:1.',
  priorities: 'Предлагаю согласовать приоритеты или решение на 1:1.',
  feedback: 'Предлагаю обсудить обратную связь по работе на 1:1.',
  overdue: 'Предлагаю обсудить просроченную договорённость на 1:1.',
};

export function MeetingSuggestion({ messageID, owner }: { messageID: string; owner: string }) {
  const query = useQuery({
    queryKey: ['meeting-suggestion', owner, messageID],
    queryFn: ({ signal }) => apiRequest('/meetings/suggestion', suggestion, { search: { message_id: messageID }, signal }),
    retry: false, staleTime: Infinity, gcTime: 0,
  });
  if (query.error) {
    if (query.error instanceof ClientError && [401, 403, 404].includes(query.error.status ?? 0)) return null;
    return <p><Link to="/meetings">Подготовить встречу вручную</Link></p>;
  }
  const draft = query.data?.data?.proposal;
  if (!draft) return null;
  return <section aria-label="Предложение встречи">
    <p>{reasons[draft.reason]}</p>
    {draft.existing_id
      ? <Link to={`/meetings#meeting-${draft.existing_id}`} state={{ owner, draft }}>Добавить темы в запланированную встречу</Link>
      : <>
        {draft.missing.length > 0 && <p>Уточните в чате или в черновике: {draft.missing.join(', ')}.</p>}
        <Link to="/meetings" state={{ owner, draft }}>Открыть черновик встречи</Link>
      </>}
  </section>;
}

export function AgendaCheckbox({ item, checked, disabled, onChange }: {
  item: AgendaEntry; checked: boolean; disabled: boolean; onChange: (included: boolean) => void;
}) {
  return <label>
    <input type="checkbox" aria-label={`Включить в повестку: ${item.text}`} checked={checked} disabled={disabled}
      onChange={event => onChange(event.target.checked)} />
    {item.text}{item.reason && ` — ${item.reason}`}
  </label>;
}

export function InvitationForm({ owner, mentor, draft, candidates, pending, send, onSent }: {
  owner: string; mentor: string; draft?: Proposal | undefined; candidates: AgendaEntry[];
  pending: boolean; send: (body: unknown) => Promise<boolean>; onSent: (id: string) => void;
}) {
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [topics, setTopics] = useState(draft?.topics.join('\n') ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || pending) return;
    const data = new FormData(event.currentTarget);
    const fields = {
      action: 'invite', mentor_id: data.get('mentor_id'), title: data.get('title'),
      local_at: `${data.get('date')}T${data.get('time')}`, timezone: String(data.get('timezone')).trim(),
      call_url: String(data.get('call_url')).trim(),
      topics: topicLines(topics),
      agenda_choices: candidates.filter(item => item.id && selected[agendaKey(item)]).map(item => ({
        kind: item.kind, id: item.id, included: true,
      })),
    };
    if (fields.topics.length > 20 || fields.topics.some(topic => topic.length > 2000)) {
      setError('Добавьте не больше 20 тем, до 2000 символов в каждой.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const storageKey = `meeting-invitation:${owner}`;
      let id = draft?.id;
      if (!id) {
        const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(fields)));
        const fingerprint = Array.from(new Uint8Array(bytes)).join('.');
        const previous = sessionStorage.getItem(storageKey)?.split(':');
        id = previous?.[0] === fingerprint && previous[1] ? previous[1] : crypto.randomUUID();
        sessionStorage.setItem(storageKey, `${fingerprint}:${id}`);
      }
      if (await send({ id, ...fields })) {
        if (!draft) sessionStorage.removeItem(storageKey);
        onSent(id);
      }
    } catch {
      setError('Не удалось подготовить отправку приглашения. Повторите попытку.');
    } finally {
      setBusy(false);
    }
  }

  return <form className={styles.form} onSubmit={event => void submit(event)}>
    <h2>{draft ? 'Черновик встречи' : 'Подготовить приглашение'}</h2>
    <label>Наставник<select name="mentor_id" required defaultValue={draft ? (draft.mentor_id === mentor ? mentor : '') : mentor}>
      <option value="">Выберите участника</option><option value={mentor}>{mentor}</option>
    </select></label>
    <label>Тема встречи<input name="title" defaultValue={draft?.title} required maxLength={200} /></label>
    <label>Дата<input name="date" type="date" defaultValue={draft?.date} required /></label>
    <label>Время<input name="time" type="time" defaultValue={draft?.time} required /></label>
    <label>Часовой пояс<input name="timezone" defaultValue={draft ? draft.timezone : timeZone} placeholder="Europe/Moscow" required maxLength={100} /></label>
    <label>Ссылка на звонок<input name="call_url" type="url" maxLength={2048} /></label>
    <fieldset className={styles.form}>
      <legend>Общая повестка</legend>
      {candidates.map(item => <AgendaCheckbox key={agendaKey(item)} item={item} checked={Boolean(selected[agendaKey(item)])}
        disabled={busy || pending} onChange={included => setSelected(values => ({ ...values, [agendaKey(item)]: included }))} />)}
      <label>Свои темы, по одной на строку<textarea value={topics} onChange={event => setTopics(event.target.value)} /></label>
      <p>Выбранные пункты и свои темы увидит наставник.</p>
    </fieldset>
    {error && <p role="alert">{error}</p>}
    <button disabled={busy || pending}>Отправить приглашение</button>
  </form>;
}

export const agendaKey = (entry: AgendaEntry) => `${entry.kind}:${entry.id ?? entry.text}`;
export const topicLines = (value: string) => value.split('\n').map(line => line.trim()).filter(Boolean);
