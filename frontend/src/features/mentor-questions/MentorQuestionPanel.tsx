import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useMutationState, useQuery, useQueryClient } from '@tanstack/react-query';
import { ClientError } from '@/shared/api';
import { Button, InlineError, Spinner, StatusBadge } from '@/shared/ui';
import { mentorQuestionsApi } from './api';
import { acceptMentorQuestion, mentorQuestionKeys } from './cache';
import { escalateMentorQuestionShared } from './escalate';
import type { MentorQuestion, MentorQuestionLookup } from './schemas';
import styles from './MentorQuestionPanel.module.css';

const POLL_INTERVAL_MS = 5000;
const ACTIVE_STATUSES: ReadonlyArray<MentorQuestion['status']> = ['new', 'in_progress'];

export interface MentorQuestionPanelProps {
  messageId: string;
  canEscalate: boolean;
  onOpenDocument: (documentId: string) => void;
  onRepeatQuestion: () => void;
  isChatBusy?: boolean;
}

function isAccessError(error: unknown): boolean {
  return error instanceof ClientError && [401, 403, 404].includes(error.status ?? 0);
}

function escalationErrorMessage(error: unknown): string | null {
  if (!(error instanceof Error) || (error instanceof ClientError && error.kind === 'aborted')) return null;
  if (error instanceof ClientError && error.kind === 'network') {
    return 'Не удалось подтвердить передачу. Проверьте соединение и повторите: сервер не создаст дубль вопроса.';
  }
  return error.message;
}

export function MentorQuestionPanel({
  messageId,
  canEscalate,
  onOpenDocument,
  onRepeatQuestion,
  isChatBusy = false,
}: MentorQuestionPanelProps) {
  const queryClient = useQueryClient();
  const [isInView, setIsInView] = useState(true);
  const rootRef = useRef<HTMLElement>(null);
  const inViewRef = useRef(true);
  const refetchRef = useRef<() => Promise<unknown>>(async () => {});
  const titleId = useId();
  const key = mentorQuestionKeys.byMessage(messageId);

  const query = useQuery({
    queryKey: key,
    enabled: canEscalate && isInView,
    queryFn: async ({ signal }) => {
      const result = await mentorQuestionsApi.get(messageId, signal);
      return acceptMentorQuestion(queryClient, messageId, result.question);
    },
  });
  refetchRef.current = () => query.refetch();

  useEffect(() => {
    const node = rootRef.current;
    if (!node || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(([entry]) => {
      const visible = entry?.isIntersecting ?? true;
      inViewRef.current = visible;
      setIsInView(visible);
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [canEscalate, query.isPending, query.isError]);

  useEffect(() => {
    if (!canEscalate || !isInView) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState !== 'visible' || !inViewRef.current) return;
      if (isAccessError(queryClient.getQueryState(key)?.error)) return;
      const status = queryClient.getQueryData<MentorQuestionLookup>(key)?.question?.status;
      if (status && ACTIVE_STATUSES.includes(status)) void refetchRef.current();
    }, POLL_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [canEscalate, isInView, key, queryClient]);

  useEffect(() => {
    const onVisibilityChange = () => {
      if (document.visibilityState !== 'visible' || !inViewRef.current) return;
      const question = queryClient.getQueryData<MentorQuestionLookup>(key)?.question;
      if (question) void refetchRef.current();
    };
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => document.removeEventListener('visibilitychange', onVisibilityChange);
  }, [key, queryClient]);

  const escalation = useMutation({
    mutationKey: mentorQuestionKeys.mutation(messageId),
    mutationFn: () => escalateMentorQuestionShared(queryClient, messageId),
  });
  const pendingMutations = useMutationState({
    filters: { mutationKey: mentorQuestionKeys.mutation(messageId), status: 'pending' },
  });
  const isEscalating = escalation.isPending || pendingMutations.length > 0;

  if (!canEscalate) return null;

  const accessError = [query.error, escalation.error].find(isAccessError);
  if (accessError) {
    return <div className={styles.state} role="status">Вопрос наставнику недоступен</div>;
  }

  if (query.isPending) {
    return (
      <div className={styles.state} role="status" aria-label="Загрузка вопроса наставнику">
        <Spinner size="sm" />
        <span>Загружаем состояние вопроса…</span>
      </div>
    );
  }

  const question = query.data?.question;
  if (!question && query.isError) {
    return (
      <div className={styles.state}>
        <InlineError
          message={query.error instanceof Error ? query.error.message : 'Не удалось загрузить состояние вопроса'}
          onRetry={() => void query.refetch()}
          isRetrying={query.isRefetching}
        />
      </div>
    );
  }

  const escalationError = escalationErrorMessage(escalation.error);
  const refreshFailed = query.isError && Boolean(question);

  if (!question) {
    return (
      <section ref={rootRef} className={styles.panel} aria-labelledby={titleId}>
        <div className={styles.header}>
          <h3 id={titleId} className={styles.title}>Помощь наставника</h3>
        </div>
        <p className={styles.description}>
          Подтверждённого ответа в базе знаний нет. Передайте вопрос наставнику — решение появится здесь.
        </p>
        {escalationError && <p className={styles.error} role="alert">{escalationError}</p>}
        <div className={styles.actions}>
          <Button
            className={styles.action}
            size="sm"
            onClick={() => escalation.mutate()}
            isLoading={isEscalating}
            disabled={isEscalating}
          >
            Передать наставнику
          </Button>
        </div>
      </section>
    );
  }

  const status = question.status === 'new'
    ? { value: 'pending' as const, label: 'Передан наставнику' }
    : question.status === 'resolved'
      ? { value: 'completed' as const, label: 'Решён' }
      : { value: 'in_progress' as const, label: 'В работе' };

  return (
    <section ref={rootRef} className={styles.panel} aria-labelledby={titleId}>
      <div className={styles.header}>
        <h3 id={titleId} className={styles.title}>Вопрос наставнику</h3>
        <StatusBadge status={status.value} label={status.label} size="sm" />
      </div>
      {question.status === 'new' && (
        <p className={styles.description}>Вопрос передан. Здесь появится решение наставника.</p>
      )}
      {question.status === 'in_progress' && (
        <p className={styles.description}>Наставник уже разбирается с вопросом.</p>
      )}
      {question.status === 'resolved' && (
        <>
          {question.resolution && <p className={styles.resolution}>{question.resolution}</p>}
          {question.document_ids.length > 0 && (
            <div className={styles.documents}>
              <p className={styles.hint}>Документы к решению</p>
              <ul className={styles.documentList}>
                {question.document_ids.map((documentId, index) => (
                  <li key={documentId}>
                    <button
                      type="button"
                      className={styles.document}
                      aria-label={`Открыть источник ${index + 1}`}
                      onClick={() => onOpenDocument(documentId)}
                    >
                      Источник {index + 1}
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}
          <div className={styles.actions}>
            <Button
              className={styles.action}
              variant="secondary"
              size="sm"
              disabled={isChatBusy}
              onClick={onRepeatQuestion}
            >
              Повторить вопрос
            </Button>
          </div>
          {isChatBusy && <p className={styles.hint}>Дождитесь завершения текущего ответа.</p>}
        </>
      )}
      {refreshFailed && (
        <p className={styles.refresh} role="status">
          Не удалось обновить статус. Показано последнее известное состояние.
        </p>
      )}
    </section>
  );
}
