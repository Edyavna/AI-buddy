import { useRef, useState, type KeyboardEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { revokeQueryAccess } from '@/shared/api/query-access';
import { Button, Dialog, EmptyState, InlineError, TextField } from '@/shared/ui';
import { mentorApi, mentorKeys } from './api';
import { accessDenied, QueryFeedback, useAction, useMentorQuery } from './common';
import type { AvailableIntern } from './models';
import styles from './Mentor.module.css';

export function AssignInternDialog({ onClose, onAssigned }: { onClose: () => void; onAssigned: (name: string) => void }) {
  const query = useMentorQuery(mentorKeys.availableInterns, mentorApi.availableInterns);
  const client = useQueryClient(), action = useAction(), root = useRef<HTMLDivElement>(null);
  const [search, setSearch] = useState(''), [selected, setSelected] = useState<AvailableIntern | null>(null);
  const [unknown, setUnknown] = useState(false);
  const availableDenied = accessDenied(query.error) || client.getQueryData(mentorKeys.availableInterns) === null;
  const items = query.data?.items.filter(i => i.name.toLocaleLowerCase('ru').includes(search.trim().toLocaleLowerCase('ru'))) ?? [];
  async function refreshLists(signal: AbortSignal) {
    signal.throwIfAborted();
    await Promise.all([mentorKeys.interns, mentorKeys.availableInterns].map(queryKey => client.cancelQueries({ queryKey, exact: true })));
    signal.throwIfAborted();
    await Promise.all([mentorKeys.interns, mentorKeys.availableInterns].map(queryKey => client.invalidateQueries({ queryKey, exact: true })));
  }
  function revoke() {
    for (const key of [mentorKeys.availableInterns, mentorKeys.interns]) {
      void client.cancelQueries({ queryKey: key, exact: true });
      revokeQueryAccess(client, key);
      client.setQueryData(key, null);
    }
    setSelected(null); setSearch(''); setUnknown(false);
  }
  function assign() {
    if (!selected) return;
    const choice = selected;
    void action.run(async signal => {
      try {
        await mentorApi.assignIntern(choice.id, AbortSignal.any([signal, AbortSignal.timeout(10_000)]));
      } catch (error) {
        if (signal.aborted) return;
        if (accessDenied(error)) { revoke(); throw new Error('Назначение недоступно. Обновите список стажёров.'); }
        if (error instanceof ClientError && error.status === 409) {
          setSelected(null); setUnknown(false);
          await refreshLists(signal);
          if (signal.aborted) return;
          throw new Error('Стажёра уже назначили. Выберите другого стажёра из обновлённого списка.');
        }
        // PUT may have committed even when its response was lost. Only a fresh
        // successful GET of our assigned list can confirm the result.
        if (!(error instanceof ClientError) || error.kind !== 'http' || (error.status ?? 0) >= 500) {
          await refreshLists(signal);
          if (signal.aborted) return;
          const state = client.getQueryState<{ items: { id: string }[] } | null>(mentorKeys.interns);
          if (accessDenied(state?.error) || client.getQueryData(mentorKeys.availableInterns) === null) {
            revoke(); throw new Error('Назначение недоступно. Обновите список стажёров.');
          }
          if (state?.status === 'success' && state.data?.items.some(i => i.id === choice.id)) {
            onAssigned(choice.name); return;
          }
          setUnknown(true);
          throw new Error('Результат назначения пока не подтверждён. Можно явно повторить то же назначение.');
        }
        throw error;
      }
      if (signal.aborted) return;
      await refreshLists(signal);
      if (!signal.aborted && [mentorKeys.interns, mentorKeys.availableInterns].some(key => accessDenied(client.getQueryState(key)?.error) || client.getQueryData(key) === null)) {
        revoke(); throw new Error('Назначение недоступно. Обновите список стажёров.');
      }
      if (!signal.aborted) onAssigned(choice.name);
    });
  }
  function trap(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Tab') return;
    const controls = root.current?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), a[href]');
    const first = controls?.[0], last = controls?.[controls.length - 1];
    if (!first || !last) return;
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }
  return <div ref={root} onKeyDown={trap} className={styles.host}>
    <Dialog isOpen onClose={onClose} title="Добавить стажёра" size="md">
      <div className={styles.page}>
        <p className={styles.muted}>Выберите свободного стажёра своей команды и подтвердите назначение себе.</p>
        <TextField label="Поиск по имени" value={search} onChange={e => setSearch(e.target.value)} disabled={action.busy || unknown} />
        <QueryFeedback query={query} />
        {query.data && (items.length ? <fieldset className={styles.candidates}><legend>Свободные стажёры</legend>{items.map(intern => <label className={styles.check} key={intern.id}>
          <input type="radio" name="intern" value={intern.id} checked={selected?.id === intern.id} onChange={() => { setSelected(intern); action.setError(''); }} disabled={action.busy || unknown} />
          <span>{intern.name}<small className={styles.candidateTeam}>{intern.team.name}</small></span>
        </label>)}</fieldset> : <EmptyState title={search.trim() ? 'Никого не нашли' : 'Свободных стажёров пока нет'} description={search.trim() ? 'Попробуйте другое имя.' : 'Все стажёры команды уже назначены.'} />)}
        {unknown && selected && !availableDenied && <p role="status">Повторить назначение: {selected.name}</p>}
        {action.error && <InlineError message={action.error} {...(availableDenied && {
          onRetry: () => void action.run(signal => refreshLists(signal)), isRetrying: action.busy,
        })} />}
        <div className={styles.row}><Button aria-label={unknown ? 'Повторить назначение' : 'Подтвердить назначение'} disabled={availableDenied || !selected || (!unknown && !query.data?.items.some(i => i.id === selected.id))} isLoading={action.busy} onClick={assign}>{unknown ? 'Повторить назначение' : 'Подтвердить назначение'}</Button><Button variant="secondary" onClick={onClose}>Отмена</Button></div>
      </div>
    </Dialog>
  </div>;
}
