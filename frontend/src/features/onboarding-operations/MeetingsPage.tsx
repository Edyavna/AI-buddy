import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { z } from 'zod';
import { useMe } from '@/features/session';
import { AgendaCheckbox, agendaEntry, agendaKey, InvitationForm, proposalLocation, topicLines, type Proposal } from './MeetingInvitation';
import { isoDate, LoadState, localDate, styles, useWorkspace } from './shared';

const agreement = z.object({
  id: z.string(), owner_id: z.string(), content: z.string(), due_at: z.string().nullable(),
  step_id: z.string().nullable(), hr_question_id: z.string().uuid().nullish(), completed_at: z.string().nullable(), revision: z.number(),
});
const meeting = z.object({
  id: z.string(), intern_id: z.string(), mentor_id: z.string(), previous_id: z.string().nullable(),
  scheduled_at: z.string(), timezone: z.string(), status: z.enum(['scheduled', 'pending', 'accepted', 'declined', 'completed', 'cancelled']),
  title: z.string().default(''), call_url: z.string().default(''),
  revision: z.number(), result: z.string(), topics: z.array(z.object({
    id: z.string(), author_id: z.string(), content: z.string(), private: z.boolean(),
  })), agenda: z.array(agendaEntry),
  agreements: z.array(agreement),
});
const workspace = z.object({
  participants: z.array(z.object({ intern_id: z.string(), mentor_id: z.string(), team_id: z.string() })),
  meetings: z.array(meeting),
  candidates: z.array(agendaEntry).default([]),
  hr_questions: z.array(z.object({ id: z.string(), title: z.string(), conversation_id: z.string(), message_id: z.string() })).default([]),
});
type Meeting = z.infer<typeof meeting>;
type Send = (payload: unknown) => Promise<boolean>;
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone ?? 'UTC';
const statuses = { scheduled: 'Запланирована', pending: 'Ожидает ответа наставника', accepted: 'Принята', declined: 'Отклонена', completed: 'Завершена', cancelled: 'Отменена' };

function MeetingCard({ item, user, send, pending, hrQuestions, draft }: { draft?: Proposal | undefined; hrQuestions: z.infer<typeof workspace>["hr_questions"]; item: Meeting; user: string; send: Send; pending: boolean }) {
  const [topic, setTopic] = useState({ id: '', content: '', private: false });
  const [suggestedTopics, setSuggestedTopics] = useState(draft?.topics.join('\n') || draft?.title || '');
  const editable = ['scheduled', 'pending', 'accepted'].includes(item.status);
  const included = item.agenda.filter(entry => entry.included);
  const [agreements, setAgreements] = useState<number[]>([]);
  const nextKey = useRef(0);
  function submit(action: string, handler: (data: FormData) => object) {
    return (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      void send({ id: item.id, revision: item.revision, action, ...handler(new FormData(event.currentTarget)) });
    };
  }
  return <article className={styles.card} id={`meeting-${item.id}`}>
    <h2>{item.title || 'Встреча'} · {new Date(item.scheduled_at).toLocaleString('ru-RU', { timeZone: item.timezone })}</h2>
    <p>{statuses[item.status]} · {item.timezone} · {item.intern_id} / {item.mentor_id}</p>
    <h3>Повестка</h3>
    {included.length ? <ul>{included.map(entry => <li key={agendaKey(entry)}>{entry.text}{entry.reason && ` — ${entry.reason}`}</li>)}</ul> : <p>Выбранных рабочих вопросов нет.</p>}
    {item.call_url && <p><a href={item.call_url} target="_blank" rel="noreferrer">Перейти к звонку</a></p>}
    {item.status === 'accepted' && <p><a href={`/api/v1/meetings/${item.id}/calendar`} download>Скачать .ics</a></p>}
    {item.status === 'pending' && user === item.mentor_id && <div className={styles.actions}>
      <button disabled={pending} onClick={() => void send({ id: item.id, revision: item.revision, action: 'accept' })}>Принять приглашение</button>
      <button disabled={pending} onClick={() => void send({ id: item.id, revision: item.revision, action: 'decline' })}>Отклонить приглашение</button>
    </div>}
    <ul>{item.topics.map(entry => <li key={entry.id}>
      {entry.content} {entry.private ? '(личная заметка)' : '(общая тема)'}
      {entry.author_id === user && editable && <>
        <button onClick={() => setTopic(entry)}>Редактировать тему</button>
        <button disabled={pending} onClick={() => void send({ action: 'topic-delete', id: item.id, revision: item.revision, topic_id: entry.id })}>Удалить тему</button>
      </>}
    </li>)}</ul>
    {editable && <>
      {item.agenda.some(entry => entry.id) && <fieldset className={styles.form}>
        <legend>Рабочие вопросы и блокеры</legend>
        {item.agenda.filter(entry => entry.id).map(entry => <AgendaCheckbox key={agendaKey(entry)} item={entry}
          checked={entry.included} disabled={pending} onChange={selected => void send({
            id: item.id, revision: item.revision, action: 'agenda',
            agenda_choices: [{ kind: entry.kind, id: entry.id, included: selected }],
          })} />)}
      </fieldset>}
      {draft && <form className={styles.form} onSubmit={event => {
        event.preventDefault();
        void send({ id: item.id, revision: item.revision, action: 'topics-add', topics: topicLines(suggestedTopics) })
          .then(ok => { if (ok) setSuggestedTopics(''); });
      }}>
        <label>Темы из чата, по одной на строку<textarea required value={suggestedTopics} onChange={event => setSuggestedTopics(event.target.value)} /></label>
        <button disabled={pending}>Добавить в повестку</button>
      </form>}
      <form className={styles.form} onSubmit={submit('details', data => ({ title: data.get('meeting-title'), call_url: data.get('meeting-url') }))}>
        <label>Тема встречи<input name="meeting-title" required maxLength={200} defaultValue={item.title} /></label>
        <label>Ссылка на звонок<input name="meeting-url" type="url" maxLength={2048} defaultValue={item.call_url} /></label>
        <button disabled={pending}>Сохранить данные встречи</button>
      </form>
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
      {item.status !== 'pending' && <form className={styles.form} onSubmit={submit('complete', (data) => ({
        content: data.get('result'),
        agreements: agreements.map((key) => ({
          content: data.get(`content-${key}`), owner_id: data.get(`owner-${key}`),
          hr_question_id: String(data.get(`hr-${key}`) ?? '').trim() || null,
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
          {user === item.intern_id && <label>Связать со своим HR-обращением<select name={`hr-${key}`}><option value="">Без связи</option>{hrQuestions.map(q => <option key={q.id} value={q.id}>{q.title}</option>)}</select></label>}
          <button type="button" onClick={() => setAgreements((rows) => rows.filter((row) => row !== key))}>Убрать договорённость</button>
        </fieldset>)}
        <div className={styles.actions}>
          <button type="button" disabled={agreements.length >= 20} onClick={() => { const key = nextKey.current++; setAgreements((rows) => [...rows, key]); }}>Добавить договорённость</button>
          <button disabled={pending}>Завершить встречу</button>
        </div>
      </form>}
    </>}
    {item.result && <><h3>Итоги</h3><p>{item.result}</p></>}
    {item.agreements.length > 0 && <h3>Договорённости</h3>}
    {item.agreements.map((entry) => <section key={entry.id} className={styles.card}>
      <p>{entry.content} · {entry.owner_id === user ? 'Вы отвечаете' : entry.owner_id} · {entry.completed_at ? 'Выполнено' : 'Открыто'}</p>
      {entry.hr_question_id && hrQuestions.filter(q => q.id === entry.hr_question_id).map(q => <Link key={q.id} to={`/chat/${encodeURIComponent(q.conversation_id)}#message-${encodeURIComponent(q.message_id)}`}>Открыть HR-обращение</Link>)}
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
  const location = useLocation();
  const navigate = useNavigate();
  const [formVersion, setFormVersion] = useState(0);
  const user = useMe().data;
  const work = useWorkspace('/meetings', workspace);
  const createId = useRef<string | undefined>(undefined);
  const decoded = proposalLocation.safeParse(location.state);
  const draft = decoded.success && decoded.data.owner === user?.id ? decoded.data.draft : undefined;
  const existing = draft?.existing_id && work.data?.meetings.some(item => item.id === draft.existing_id &&
    ['scheduled', 'pending', 'accepted'].includes(item.status)) ? draft.existing_id : undefined;
  const load = work.load;
  useEffect(() => {
    if (work.pending || user?.role !== 'intern' && user?.role !== 'mentor') return;
    const refresh = () => { if (!document.hidden) void load(true); };
    const timer = window.setInterval(refresh, 15000);
    window.addEventListener('focus', refresh);
    return () => { window.clearInterval(timer); window.removeEventListener('focus', refresh); };
  }, [load, user?.role, work.pending]);
  const focused = work.data?.meetings.some(item => location.hash === `#meeting-${item.id}`);
  useEffect(() => {
    if (focused) document.getElementById(location.hash.slice(1))?.scrollIntoView?.();
  }, [focused, location.hash]);
  return <section className={styles.page}>
    <h1>Встречи 1:1</h1>
    {work.message && <p role="alert">{work.message}</p>}
    <LoadState loading={work.loading} error={work.error} retry={() => void work.load()}>
      {!work.data?.participants.length ? <p>Для встречи нужно действующее назначение наставника.</p> :
      user?.role === 'intern' ? <InvitationForm key={`${user.id}:${draft?.id ?? ''}:${formVersion}`} owner={user.id}
        mentor={work.data.participants[0]!.mentor_id} draft={existing ? undefined : draft}
        candidates={work.data.candidates} pending={work.pending} send={work.send} onSent={id => {
          setFormVersion(version => version + 1);
          navigate(`/meetings#meeting-${id}`, { replace: true, state: null });
        }} /> :
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
      {work.data?.meetings.map((item) => <MeetingCard key={`${item.id}:${existing === item.id ? draft?.id : ''}`} item={item} draft={existing === item.id ? draft : undefined} hrQuestions={work.data?.hr_questions ?? []} user={user?.id ?? ''} send={work.send} pending={work.pending} />)}
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
