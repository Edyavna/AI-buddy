import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { Button, InlineError } from '@/shared/ui';
import { useAccessScope, useMe } from '@/features/session';
import { DocumentDrawer } from '@/features/documents';
import { hrApi, questionStatus, type QuestionAction, type RoutedQuestion } from './api';
import { useOperation, usePrivateQuery } from './requests';
import styles from './HR.module.css';

export function HRQuestionsPage() {
  const me = useMe();
  const scope = useAccessScope(me.data?.id);
  const [team, setTeam] = useState('');
  const [status, setStatus] = useState('');
  const [q, setQuery] = useState('');
  const teamId = scope.data?.teams.some(t => t.id === team) ? team : '';
  const query = usePrivateQuery(['hr', 'questions', teamId, status, q],
    signal => hrApi.questions({ team_id: teamId, status, q }, signal), true, 5000);

  return <section className={styles.page}>
    <h1>Кадровые обращения</h1>
    <div className={styles.form}>
      <label>Команда<select value={teamId} onChange={event => setTeam(event.target.value)}>
        <option value="">Все обслуживаемые команды</option>
        {scope.data?.teams.map(t => <option value={t.id} key={t.id}>{t.name}</option>)}
      </select></label>
      <label>Статус<select value={status} onChange={event => setStatus(event.target.value)}>
        <option value="">Все статусы</option>
        {Object.entries(questionStatus).map(([value, label]) => <option value={value} key={value}>{label}</option>)}
      </select></label>
      <label>Поиск<input type="search" value={q} onChange={event => setQuery(event.target.value)} /></label>
    </div>
    {query.isError && <InlineError message={query.error.message} onRetry={() => void query.refetch()} />}
    {query.isPending && <p role="status">Загрузка обращений…</p>}
    {query.data && (query.data.items.length ? <ul className={styles.list}>
      {query.data.items.map(item => <li key={item.id} className={styles.card}>
        <Link to={encodeURIComponent(item.id)}>{item.question}</Link>
        <p>{item.student.name} · {questionStatus[item.status]}</p>
      </li>)}
    </ul> : <p>Обращений пока нет.</p>)}
  </section>;
}

export function HRQuestionPage() {
  const { id = '' } = useParams();
  const query = usePrivateQuery(['hr', 'question', id], signal => hrApi.question(id, signal), true, 5000);
  return <section className={styles.page}>
    <Link to="/hr/questions">К кадровым обращениям</Link>
    {query.isError && <InlineError message={query.error.message} onRetry={() => void query.refetch()} />}
    {query.isPending && <p role="status">Загрузка обращения…</p>}
    {query.data && <QuestionDetail key={`${id}:${query.data.revision}`} item={query.data} />}
  </section>;
}

function QuestionDetail({ item }: { item: RoutedQuestion }) {
  const key = ['hr', 'question', item.id];
  const me = useMe();
  const scope = useAccessScope(me.data?.id);
  const docs = usePrivateQuery(['hr', 'documents'], hrApi.documents);
  const client = useQueryClient();
  const op = useOperation(key);
  const [response, setResponse] = useState(item.resolution ?? '');
  const [selected, setSelected] = useState(item.document_ids);
  const [openDocument, setOpenDocument] = useState<string>();
  const owner = item.specialist_id === me.data?.id;
  const names = scope.data?.teams;
  const canAnswer = item.status === 'in_progress' && owner;

  async function apply(action: QuestionAction) {
    const details = action === 'answer' || action === 'resolve' ? { resolution: response, document_ids: selected } : {};
    const body = { action, revision: item.revision, ...details };
    const result = await op.run(body, (operation_id, signal) => hrApi.update(item.id, { operation_id, ...body }, signal),
      async signal => {
        const latest = await hrApi.question(item.id, signal);
        const status = action === 'resolve' ? 'resolved' : 'in_progress';
        return latest.revision > item.revision && latest.status === status && latest.specialist_id === me.data?.id
          && (!details.resolution || (latest.resolution === details.resolution.trim() && latest.document_ids.join() === selected.join())) ? latest : undefined;
      });
    if (!result) return;
    client.setQueryData(key, result);
    await client.invalidateQueries({ queryKey: ['hr', 'questions'] });
  }

  return <>
    <h1>{item.question}</h1>
    <p>{item.student.name} · {names?.find(t => t.id === item.team_id)?.name ?? item.team_id} · {questionStatus[item.status]}</p>
    {item.context_excerpt && <div className={styles.card}><strong>Контекст</strong><p>{item.context_excerpt}</p></div>}
    {item.resolution && <div className={styles.card}><strong>Ответ</strong><p>{item.resolution}</p>
      {item.document_ids.map(id => <Button key={id} variant="secondary" onClick={() => setOpenDocument(id)}>Открыть инструкцию</Button>)}
    </div>}
    {item.status === 'new' && <Button isLoading={op.pending} onClick={() => void apply('claim')}>Взять в работу</Button>}
    {item.status === 'resolved' && <><Button isLoading={op.pending} onClick={() => void apply('reopen')}>Вернуть в работу</Button><Link to={`/hr/questions/${encodeURIComponent(item.id)}/instruction`}>Подготовить инструкцию из решения</Link></>}
    {canAnswer && <form className={styles.form} onSubmit={event => { event.preventDefault(); void apply('answer'); }}>
      <label>Ответ стажёру<textarea required rows={5} value={response} onChange={event => setResponse(event.target.value)} /></label>
      <fieldset><legend>Инструкции к ответу</legend>
        {docs.data?.items.filter(d => d.published_version === d.version && !d.revoked && d.team_ids.includes(item.team_id)).map(d =>
          <label key={d.id}><span><input type="checkbox" checked={selected.includes(d.id)}
            onChange={event => setSelected(event.target.checked ? [...selected, d.id] : selected.filter(id => id !== d.id))} /> {d.title}</span></label>)}
      </fieldset>
      <div className={styles.actions}>
        <Button type="submit" isLoading={op.pending}>Отправить ответ</Button>
        <Button type="button" variant="secondary" disabled={op.pending || !response.trim()} onClick={() => void apply('resolve')}>Решить обращение</Button>
      </div>
    </form>}
    {item.status === 'in_progress' && !owner && <p>Обращение обрабатывает другой HR.</p>}
    {op.error && <InlineError message={op.error} />}
    {openDocument && <DocumentDrawer documentId={openDocument} onClose={() => setOpenDocument(undefined)} />}
  </>;
}
