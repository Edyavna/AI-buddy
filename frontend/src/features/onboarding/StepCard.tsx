import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Link } from 'react-router-dom';
import { Button, StatusBadge } from '@/shared/ui';
import { AccessRequestPanel } from '@/features/access-requests';
import type { CheckpointConfirmation, Step, StepTarget } from './schemas';
import styles from './StepCard.module.css';

const TARGET_LABEL: Record<StepTarget, (step: Step) => string> = {
  in_progress: (step) => (step.status === 'completed' ? 'Вернуть в работу' : 'Начать шаг'),
  completed: () => 'Отметить выполненным',
  not_started: () => 'Сбросить в не начатые',
};

const POLICY_LABEL: Record<Step['completion_policy'], string> = {
  intern: 'Подтверждает стажёр',
  mentor: 'Подтверждает наставник',
  both: 'Подтверждают стажёр и наставник',
};

function ChatIcon() {
  return (
    <svg className={styles.buttonIcon} viewBox="0 0 20 20" fill="none" aria-hidden="true">
      <path d="M4 15.5 2.5 18v-5.2A7 7 0 0 1 1 9a7 7 0 0 1 7-7h4a7 7 0 0 1 0 14H8.5L4 15.5Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
    </svg>
  );
}

function ArrowIcon({ className = '' }: { className?: string }) {
  return (
    <svg className={`${styles.buttonIcon} ${className}`} viewBox="0 0 20 20" fill="none" aria-hidden="true">
      <path d="M4 10h11m-4-4 4 4-4 4" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function ActionIcon({ action }: { action: StepTarget | 'confirm' | 'revoke' }) {
  const isCheck = action === 'completed' || action === 'confirm';
  return (
    <svg className={styles.buttonIcon} viewBox="0 0 20 20" fill="none" aria-hidden="true">
      {isCheck ? (
        <path d="m4.5 10 3.5 3.5 7.5-8" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
      ) : action === 'in_progress' ? (
        <path d="M4 10h11m-4-4 4 4-4 4" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
      ) : (
        <path d="M5.5 6.5A6.5 6.5 0 1 1 3.7 12M3.5 5v4h4" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
      )}
    </svg>
  );
}

function safeUrl(value: string | null | undefined): string | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password ? url.href : null;
  } catch {
    return null;
  }
}

function ConfirmationLine({ role, confirmation }: { role: 'intern' | 'mentor'; confirmation: CheckpointConfirmation | undefined }) {
  const owner = role === 'intern' ? 'Стажёр' : 'Наставник';
  return (
    <li className={styles.confirmationLine}>
      <span>{owner}</span>
      {confirmation ? (
        <span className={styles.confirmationDone}>
          Подтверждено{' '}
          <time dateTime={confirmation.confirmed_at}>
            {new Intl.DateTimeFormat('ru-RU', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(confirmation.confirmed_at))}
          </time>
        </span>
      ) : (
        <span>Ожидается</span>
      )}
    </li>
  );
}

export interface StepCardProps {
  step: Step;
  index: number;
  isNext: boolean;
  isUpdating: boolean;
  isCheckpointUpdating?: boolean;
  error?: string | null;
  onStatusChange: (stepId: string, target: StepTarget) => void;
  onConfirmationChange?: (stepId: string, confirmed: boolean, revision: number) => void;
}

export const StepCard: React.FC<StepCardProps> = ({
  step,
  index,
  isNext,
  isUpdating,
  isCheckpointUpdating = false,
  error,
  onStatusChange,
  onConfirmationChange,
}) => {
  const chatSearch = new URLSearchParams({ stepId: step.id }).toString();
  const isBlocked = step.status === 'blocked';
  const hasCheckpointCompletion = step.completion_policy !== 'intern' ||
    step.confirmations.length > 0 || step.allowed_actions.length > 0;
  const statusTargets = hasCheckpointCompletion
    ? step.allowed_statuses.filter((target) => target !== 'completed' && !(step.status === 'completed' && target === 'in_progress'))
    : step.allowed_statuses;
  const internConfirmation = step.confirmations.find((item) => item.role === 'intern');
  const mentorConfirmation = step.confirmations.find((item) => item.role === 'mentor');

  return (
    <article
      className={`${styles.stepCard} ${isNext ? styles.stepCardActive : ''}`}
      data-step-id={step.id}
    >
      <div className={styles.stepCardHeader}>
        <div className={styles.stepTitleGroup}>
          <span className={`${styles.stepNumber} ${isBlocked ? styles.stepNumberBlocked : ''}`} aria-hidden="true">
            {index + 1}
          </span>
          <div>
            <h3 className={styles.stepTitle}>{step.title}</h3>
            {step.period && <p className={styles.stepPeriod}>Период: {step.period}</p>}
          </div>
        </div>
        <StatusBadge status={step.status} size="sm" />
      </div>

      {step.description && step.description !== step.instructions && step.description !== step.title && (
        <p className={styles.stepDescription}>{step.description}</p>
      )}

      {step.instructions && step.instructions !== step.title && (
        <section className={styles.instructions} aria-label={`Инструкция: ${step.title}`}>
          <h4 className={styles.detailsTitle}>Инструкция</h4>
          <div className={styles.markdown}>
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{
                img: ({ alt }) => <span>{alt || 'Изображение'}</span>,
                a: ({ href, children }) => {
                  const url = safeUrl(href);
                  return url
                    ? <a href={url} target="_blank" rel="noreferrer noopener">{children}</a>
                    : <span>{children}</span>;
                },
              }}
            >
              {step.instructions}
            </ReactMarkdown>
          </div>
        </section>
      )}

      {step.resources.length > 0 && (
        <section className={styles.resources} aria-label={`Материалы: ${step.title}`}>
          <h4 className={styles.detailsTitle}>Материалы</h4>
          <ul className={styles.resourcesList}>
            {step.resources.map((resource, resourceIndex) => {
              const url = safeUrl(resource.url);
              return (
                <li key={`${resource.title}-${resourceIndex}`} className={styles.resourceItem}>
                  {url ? (
                    <a className={styles.resourceLink} href={url} target="_blank" rel="noreferrer noopener">
                      {resource.title}
                    </a>
                  ) : (
                    <span className={styles.resourceName}>{resource.title}</span>
                  )}
                  {!url && <span className={styles.resourceNote}>{resource.note ?? 'Уточнить у наставника'}</span>}
                  {url && resource.note && <span className={styles.resourceNote}>{resource.note}</span>}
                </li>
              );
            })}
          </ul>
        </section>
      )}

      <section className={styles.completion} aria-label={`Завершение: ${step.title}`}>
        <h4 className={styles.detailsTitle}>{POLICY_LABEL[step.completion_policy]}</h4>
        {step.completion_policy === 'both' ? (
          <ul className={styles.confirmationsList}>
            <ConfirmationLine role="intern" confirmation={internConfirmation} />
            <ConfirmationLine role="mentor" confirmation={mentorConfirmation} />
          </ul>
        ) : (
          (step.completion_policy === 'intern' ? internConfirmation : mentorConfirmation) && (
            <p className={styles.singleConfirmation}>
              {step.completion_policy === 'intern' ? 'Вы подтвердили' : 'Наставник подтвердил'}{' '}
              <time dateTime={(step.completion_policy === 'intern' ? internConfirmation : mentorConfirmation)!.confirmed_at}>
                {new Intl.DateTimeFormat('ru-RU', { dateStyle: 'medium', timeStyle: 'short' }).format(
                  new Date((step.completion_policy === 'intern' ? internConfirmation : mentorConfirmation)!.confirmed_at),
                )}
              </time>
            </p>
          )
        )}
        {step.awaiting_role === 'mentor' && <p className={styles.awaiting}>Ожидаем наставника</p>}
        {step.awaiting_role === 'intern' && (
          <p className={styles.awaiting}>Наставник уже подтвердил. Осталось ваше подтверждение.</p>
        )}
      </section>

      {isBlocked && step.blocked_reason && (
        <p className={styles.blockedReason}>{step.blocked_reason}</p>
      )}

      {error && <p className={styles.stepError} role="alert">{error}</p>}

      {step.request_ids.length > 0 && (
        <div className={styles.requestsGroup}>
          <h4 className={styles.requestsTitle}>Заявки на доступ</h4>
          {step.request_ids.map((requestId) => (
            <AccessRequestPanel key={requestId} requestId={requestId} />
          ))}
        </div>
      )}

      <div className={styles.stepFooter}>
        <Link
          to={`/chat?${chatSearch}`}
          className={styles.askLink}
          aria-label={`Спросить помощника о шаге: ${step.title}`}
        >
          <Button
            variant={isNext ? 'primary' : 'secondary'}
            size="sm"
            leftIcon={<ChatIcon />}
            rightIcon={<ArrowIcon className={styles.askArrow ?? ''} />}
          >
            Спросить помощника
          </Button>
        </Link>

        {!isBlocked && statusTargets.map((target) => (
          <Button
            key={target}
            variant="secondary"
            size="sm"
            disabled={isUpdating || isCheckpointUpdating}
            leftIcon={<ActionIcon action={target} />}
            onClick={() => onStatusChange(step.id, target)}
          >
            {TARGET_LABEL[target](step)}
          </Button>
        ))}

        {onConfirmationChange && step.allowed_actions.map((action) => (
          <Button
            key={action}
            variant={action === 'confirm' ? 'primary' : 'secondary'}
            size="sm"
            disabled={isUpdating || isCheckpointUpdating}
            leftIcon={<ActionIcon action={action} />}
            onClick={() => onConfirmationChange(step.id, action === 'confirm', step.revision)}
          >
            {action === 'confirm' ? 'Подтвердить выполнение' : 'Отозвать подтверждение'}
          </Button>
        ))}
      </div>
    </article>
  );
};
