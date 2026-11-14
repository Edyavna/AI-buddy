import { useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { QueryFeedback, useAction, useMentorQuery } from '@/features/mentor/common';
import { queryRequestSignal } from '@/shared/api/query-access';
import { Button, InlineError } from '@/shared/ui';
import { invitationKey, loadInvitation, setInvitation } from './api';
import styles from './ActionQueue.module.css';

export function CheckpointInvitation({ step }: { step: string }) {
  const client = useQueryClient();
  const action = useAction();
  const requestID = useRef<string>(null);
  const key = invitationKey(step);
  const query = useMentorQuery(key, async signal => ({ invitation: await loadInvitation(step, signal) }));
  const current = query.data?.invitation;
  const pending = current?.state === 'pending';
  const completed = current?.state === 'completed';

  function change() {
    const id = pending ? current.id : (requestID.current ??= crypto.randomUUID());
    void action.run(async owner => {
      const signal = queryRequestSignal(client, key, owner);
      try {
        const value = await setInvitation(step, id, pending ? 'cancel' : 'request', signal);
        await client.cancelQueries({ queryKey: key, exact: true });
        signal.throwIfAborted();
        client.setQueryData(key, { invitation: value });
        requestID.current = null;
      } finally {
        if (!signal.aborted) {
          const refreshed = await query.refetch();
          if (refreshed.data?.invitation?.id === id || refreshed.data?.invitation?.state === 'pending') {
            requestID.current = null;
          }
        }
      }
    });
  }

  return <section className={styles.checkpoint} aria-label="Приглашение в закрытые чаты">
    <h4>Приглашение в закрытые чаты</h4>
    <QueryFeedback query={query} />
    {action.error && <InlineError message={action.error} />}
    {pending && <p role="status">Наставник получил запрос на ручное приглашение.</p>}
    {completed && <p role="status">Наставник отметил, что пригласил вас вручную.{current.note && <> {current.note}</>}</p>}
    {query.data && !query.isError && <Button variant="secondary" size="sm" onClick={change} disabled={action.busy}>
      {pending ? 'Отменить запрос' : completed ? 'Запросить ещё одно приглашение' : 'Попросить приглашение'}
    </Button>}
  </section>;
}
