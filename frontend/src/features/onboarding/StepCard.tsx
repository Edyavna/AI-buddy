import React from 'react';
import { Link } from 'react-router-dom';
import { Button, StatusBadge } from '@/shared/ui';
import { AccessRequestPanel } from '@/features/access-requests';
import type { Step, StepTarget } from './schemas';
import styles from './StepCard.module.css';

const TARGET_LABEL: Record<StepTarget, (step: Step) => string> = {
  in_progress: (step) => (step.status === 'completed' ? 'Вернуть в работу' : 'Начать шаг'),
  completed: () => 'Отметить выполненным',
  not_started: () => 'Сбросить в не начатые',
};

export interface StepCardProps {
  step: Step;
  index: number;
  isNext: boolean;
  isUpdating: boolean;
  error?: string | null;
  onStatusChange: (stepId: string, target: StepTarget) => void;
}

export const StepCard: React.FC<StepCardProps> = ({
  step,
  index,
  isNext,
  isUpdating,
  error,
  onStatusChange,
}) => {
  const chatSearch = new URLSearchParams({ stepId: step.id }).toString();
  const isBlocked = step.status === 'blocked';

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
          <h3 className={styles.stepTitle}>{step.title}</h3>
        </div>
        <StatusBadge status={step.status} size="sm" />
      </div>

      <p className={styles.stepDescription}>{step.description}</p>

      {isBlocked && step.blocked_reason && (
        <p className={styles.blockedReason}>{step.blocked_reason}</p>
      )}

      {error && (
        <p className={styles.stepError} role="alert">{error}</p>
      )}

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
          <Button variant={isNext ? 'primary' : 'secondary'} size="sm">
            Спросить помощника
          </Button>
        </Link>

        {!isBlocked &&
          step.allowed_statuses.map((target) => (
            <Button
              key={target}
              variant="secondary"
              size="sm"
              disabled={isUpdating}
              onClick={() => onStatusChange(step.id, target)}
            >
              {TARGET_LABEL[target](step)}
            </Button>
          ))}
      </div>
    </article>
  );
};
