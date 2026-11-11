import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, InlineError } from '@/shared/ui';
import { accessDenied, getQuestionOptions, sendQuestion } from './mentor-question';
import styles from './MentorQuestionPanel.module.css';

const labels = { new: 'Передан наставнику', in_progress: 'В работе', resolved: 'Решён' };

interface Props {
  messageId: string;
  onOpenDocument: (id: string) => void;
  onRepeatQuestion: () => void;
  repeatDisabled: boolean;
}

export function MentorQuestionPanel({ messageId, onOpenDocument, onRepeatQuestion, repeatDisabled }: Props) {
  const client = useQueryClient();
  const mutationKey = ['chat', 'escalate', messageId] as const;
  const busy = useIsMutating({ mutationKey, exact: true }) > 0;
  const query = useQuery({
    ...getQuestionOptions(client, messageId),
    refetchInterval: q => {
      if (accessDenied(q.state.error) || !q.state.data) return false;
      // Resolved cards still need to notice that the question was reopened.
      return q.state.data.status === 'resolved' ? 15_000 : 5000;
    },
    refetchIntervalInBackground: false,
  });
  const transfer = useMutation({ mutationKey, mutationFn: () => sendQuestion(client, messageId) });
  const denied = accessDenied(query.error) || accessDenied(transfer.error);
  const question = denied ? undefined : query.data;
  const error = query.error ?? transfer.error;

  return <section className={styles.panel} aria-label="Передача вопроса наставнику">
    {query.isPending && <p role="status">Проверяем статус передачи…</p>}
    {error && <InlineError
      message={denied ? 'Вопрос недоступен' : error.message}
      {...(query.isError || denied ? { onRetry: () => { transfer.reset(); void query.refetch(); } } : {})}
      isRetrying={query.isFetching}
    />}
    {!denied && question === null && !query.isError && (
      <Button variant="secondary" isLoading={busy} onClick={() => transfer.mutate()}>
        Передать наставнику
      </Button>
    )}
    {question && <p role="status">{labels[question.status]}</p>}
    {question?.status === 'resolved' && <>
      {question.resolution && <p>{question.resolution}</p>}
      <div className={styles.actions}>
        {question.document_ids.map((id, index) => (
          <Button key={id} variant="secondary" onClick={() => onOpenDocument(id)}>
            {question.document_ids.length === 1 ? 'Открыть документ' : `Открыть документ ${index + 1}`}
          </Button>
        ))}
        <Button variant="secondary" disabled={repeatDisabled} onClick={onRepeatQuestion}>Повторить вопрос</Button>
      </div>
    </>}
  </section>;
}
