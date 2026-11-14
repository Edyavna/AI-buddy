import { useRef, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { z } from 'zod';
import { useMe } from '@/features/session';
import { isoDate, LoadState, localDate, styles, useWorkspace } from './shared';

const agreement = z.object({
  id: z.string(), owner_id: z.string(), content: z.string(), due_at: z.string().nullable(),
  step_id: z.string().nullable(), completed_at: z.string().nullable(), revision: z.number(),
});
const meeting = z.object({
  id: z.string(), intern_id: z.string(), mentor_id: z.string(), previous_id: z.string().nullable(),
  scheduled_at: z.string(), timezone: z.string(), status: z.enum(['scheduled', 'completed', 'cancelled']),
  revision: z.number(), result: z.string(), topics: z.array(z.object({
    id: z.string(), author_id: z.string(), content: z.string(), private: z.boolean(),
  })), agenda: z.array(z.object({ kind: z.string(), text: z.string(), step_id: z.string().nullable(), state: z.string(), reason: z.string().nullable().optional() })),
  agreements: z.array(agreement),
});
const workspace = z.object({
  participants: z.array(z.object({ intern_id: z.string(), mentor_id: z.string(), team_id: z.string() })),
  meetings: z.array(meeting),
});
type Meeting = z.infer<typeof meeting>;
type Send = (payload: unknown) => Promise<boolean>;
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone ?? 'UTC';
const statuses = { scheduled: 'Запланирована', completed: 'Завершена', cancelled: 'Отменена' };

function MeetingCard({ item, user, send, pending }: { item: Meeting; user: string; send: Send; pending: boolean }) {
  const [topic, setTopic] = useState({ id: '', content: '', private: false });
  const [agreements, setAgreements] = useState<number[]>([]);
  const nextKey = useRef(0);
  function submit(action: string, handler: (data: FormData) => object) {
    return (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      void send({ id: item.id, revision: item.revision, action, ...handler(new FormData(event.currentTarget)) });
    };
  }
  return <article className={styles.card}>
    <h2>Встреча {new Date(item.scheduled_at).toLocaleString('ru-RU', { timeZone: item.timezone })}</h2>
    <p>{statuses[item.status]} · {item.timezone} · {item.intern_id} / {item.mentor_id}</p>
    <h3>Повестка</h3>
    {item.agenda.length ? <ul>{item.agenda.map((entry, index) => <li key={index}>{entry.text}{entry.reason && ` — ${entry.reason}`}</li>)}</ul> : <p>Новых тем нет.</p>}
    <ul>{item.topics.map((entry) => <li key={entry.id}>
      {entry.content} {entry.private ? '(личная заметка)' : '(общая тема)'}
      {entry.author_id === user && item.status === 'scheduled' && <button onClick={() => setTopic(entry)}>Редактировать тему</button>}
    </li>)}</ul>
    {item.status === 'scheduled' && <>
      <form className={styles.form} onSubmit={(event) => {
        event.preventDefault();
        const id = topic.id || crypto.randomUUID();
        setTopic({ ...topic, id });
        void send({ id: item.id, revision: item.revision, action: 'topic', topic_id: id, content: topic.content, private: topic.private })
          .then((ok) => { if (ok) setTopic({ id: '', content: '', private: false }); });
      }}>
        <label>Тема или личная заметка<textarea required maxLength={2000} value={topic.content} onChange={(e) => setTopic({ ...topic, content: e.target.value })} /></label>
        <label><input type="checkbox" checked={topic.private} onChange={(e) => setTopic({ ...topic, private: e.target.checked })} />Видна только мне</label>
        {!topic.private && <p>Общую тему увидят стажёр и назначенный наставник. Перед сохранением проверьте, какой фрагмент вы хотите передать.</p>}
        <div className={styles.actions}><button disabled={pending}>Сохранить тему</button><button type="button" onClick={() => setTopic({ id: '', content: '', private: false })}>Новая тема</button></div>
      </form>
      <form className={styles.form} onSubmit={submit('reschedule', (data) => ({ scheduled_at: isoDate(String(data.get('scheduled_at'))), timezone }))}>
        <label>Новая дата и время ({timezone})<input name="scheduled_at" type="datetime-local" defaultValue={localDate(item.scheduled_at)} required /></label>
        <div className={styles.actions}><button disabled={pending}>Перенести встречу</button><button type="button" disabled={pending} onClick={() => void send({ id: item.id, revision: item.revision, action: 'cancel' })}>Отменить встречу</button></div>
      </form>
      <form className={styles.form} onSubmit={submit('complete', (data) => ({
        content: data.get('result'),
        agreements: agreements.map((key) => ({
          content: data.get(`content-${key}`), owner_id: data.get(`owner-${key}`),
          due_at: isoDate(String(data.get(`due-${key}`) ?? '')), step_id: data.get(`step-${key}`) || null,
        })),
      }))}>
        <label>Итоги встречи<textarea name="result" maxLength={4000} required /></label>
        {agreements.map((key) => <fieldset key={key} className={styles.form}>
          <legend>Договорённость</legend>
          <label>Текст<textarea name={`content-${key}`} maxLength={2000} required /></label>
          <label>Ответственный<select name={`owner-${key}`}><option value={item.intern_id}>Стажёр</option><option value={item.mentor_id}>Наставник</option></select></label>
          <label>Срок<input name={`due-${key}`} type="datetime-local" /></label>
          <label>Чекпоинт<select name={`step-${key}`}><option value="">Без связи</option>{item.agenda.filter((entry) => entry.kind === 'checkpoint').map((entry) => <option key={entry.step_id} value={entry.step_id ?? ''}>{entry.text}</option>)}</select></label>
          <button type="button" onClick={() => setAgreements((rows) => rows.filter((row) => row !== key))}>Убрать договорённость</button>
        </fieldset>)}
        <div className={styles.actions}>
          <button type="button" disabled={agreements.length >= 20} onClick={() => { const key = nextKey.current++; setAgreements((rows) => [...rows, key]); }}>Добавить договорённость</button>
          <button disabled={pending}>Завершить встречу</button>
        </div>
      </form>
    </>}
    {item.result && <><h3>Итоги</h3><p>{item.result}</p></>}
    {item.agreements.length > 0 && <h3>Договорённости</h3>}
    {item.agreements.map((entry) => <section key={entry.id} className={styles.card}>
      <p>{entry.content} · {entry.owner_id === user ? 'Вы отвечаете' : entry.owner_id} · {entry.completed_at ? 'Выполнено' : 'Открыто'}</p>
      {entry.due_at && <p>Срок {new Date(entry.due_at).toLocaleString('ru-RU')}</p>}
      {entry.owner_id === user && !entry.completed_at && <form className={styles.form} onSubmit={(event) => {
        event.preventDefault();
        void send({ id: entry.id, revision: entry.revision, action: 'agreement-reschedule', due_at: isoDate(String(new FormData(event.currentTarget).get('due_at') ?? '')) });
      }}>
        <label>Новый срок<input name="due_at" type="datetime-local" defaultValue={entry.due_at ? localDate(entry.due_at) : ''} /></label>
        <div className={styles.actions}><button disabled={pending}>Сохранить срок</button><button type="button" disabled={pending} onClick={() => void send({ id: entry.id, revision: entry.revision, action: 'agreement-complete' })}>Выполнено</button></div>
      </form>}
    </section>)}
  </article>;
}

export function MeetingsPage() {
  const user = useMe().data;
  const work = useWorkspace('/meetings', workspace);
  const createId = useRef<string | undefined>(undefined);
  return <section className={styles.page}>
    <h1>Встречи 1:1</h1>
    {work.message && <p role="alert">{work.message}</p>}
    <LoadState loading={work.loading} error={work.error} retry={() => void work.load()}>
      {!work.data?.participants.length ? <p>Для встречи нужно действующее назначение наставника.</p> :
        <form className={styles.form} onSubmit={(event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          createId.current ??= crypto.randomUUID();
          void work.send({ action: 'create', id: createId.current, intern_id: data.get('intern_id'), scheduled_at: isoDate(String(data.get('scheduled_at'))), timezone })
            .then((ok) => { if (ok) createId.current = undefined; });
        }}>
          <label>Стажёр<select name="intern_id" onChange={() => { createId.current = undefined; }}>{work.data.participants.map((row) => <option key={row.intern_id}>{row.intern_id}</option>)}</select></label>
          <label>Дата и время ({timezone})<input type="datetime-local" name="scheduled_at" required onChange={() => { createId.current = undefined; }} /></label>
          <button disabled={work.pending}>Запланировать встречу</button>
        </form>}
      {!work.data?.meetings.length && <p>Встреч пока нет.</p>}
      {work.data?.meetings.map((item) => <MeetingCard key={item.id} item={item} user={user?.id ?? ''} send={work.send} pending={work.pending} />)}
    </LoadState>
  </section>;
}

function AgreementList({ internID }: { internID: string | undefined }) {
  const user = useMe().data;
  const work = useWorkspace('/meetings', workspace);
  const entries = work.data?.meetings.filter((item) => !internID || item.intern_id === internID).flatMap((item) => item.agreements) ?? [];
  return <LoadState loading={work.loading} error={work.error} retry={() => void work.load()}>
    {!entries.length && <p>Договорённостей пока нет.</p>}
    <ul>{entries.map((entry) => <li key={entry.id}>
      {entry.content} · {entry.completed_at ? 'Выполнено' : 'Открыто'} · {entry.owner_id === user?.id ? 'Вы отвечаете' : entry.owner_id}
      {entry.due_at && <> · срок {new Date(entry.due_at).toLocaleString('ru-RU')}</>}
      {entry.owner_id === user?.id && !entry.completed_at && <button disabled={work.pending} onClick={() => void work.send({
        id: entry.id, revision: entry.revision, action: 'agreement-complete',
      })}>Выполнено</button>}
    </li>)}</ul>
    {work.message && <p role="alert">{work.message}</p>}
  </LoadState>;
}

export function PlanAgreements({ internID, mentor = false }: { internID?: string; mentor?: boolean }) {
  const [expanded, setExpanded] = useState(false);
  return <section className={styles.card} aria-label="Договорённости по плану">
    <h2>Договорённости по плану</h2>
    <div className={styles.actions}>
      <button aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? 'Скрыть договорённости' : 'Показать договорённости'}</button>
      <Link to={mentor ? '/mentor/meetings' : '/meetings'}>К встречам 1:1</Link>
    </div>
    {expanded && <AgreementList internID={internID} />}
  </section>;
}
