import React from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { StatusBadge, type BadgeStatus } from '@/shared/ui';
import type { Recommendation, RecommendationState } from './schemas';
import styles from './RecommendationCard.module.css';

export interface RecommendationCardProps {
  recommendation: Recommendation;
  onSelectStep?: (stepId: string) => void;
}

export function formatWaitingDuration(seconds: number | null | undefined): string | null {
  if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return null;
  if (seconds < 60) return 'меньше минуты';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} мин`;
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  if (hours < 24) {
    return remMinutes > 0 ? `${hours} ч ${remMinutes} мин` : `${hours} ч`;
  }
  const days = Math.floor(hours / 24);
  const remHours = hours % 24;
  return remHours > 0 ? `${days} дн ${remHours} ч` : `${days} дн`;
}

const STATE_CONFIG: Record<
  RecommendationState,
  { status: BadgeStatus; label: string }
> = {
  action_available: { status: 'in_progress', label: 'Рекомендуемый шаг' },
  waiting_mentor: { status: 'pending', label: 'Ожидает наставника' },
  waiting_access: { status: 'pending', label: 'Ожидает доступов' },
  blocked_dependencies: { status: 'blocked', label: 'Заблокировано зависимостями' },
  blocked: { status: 'blocked', label: 'Заблокирован' },
  completed: { status: 'completed', label: 'Все задачи выполнены' },
  empty_plan: { status: 'not_started', label: 'План пуст' },
};

export const RecommendationCard: React.FC<RecommendationCardProps> = ({
  recommendation,
  onSelectStep,
}) => {
  const navigate = useNavigate();
  const {
    state,
    title,
    period,
    reason,
    action_label,
    target,
    waiting_step,
    help_action,
  } = recommendation;

  const badgeConfig = STATE_CONFIG[state] ?? {
    status: 'neutral' as BadgeStatus,
    label: 'Рекомендация',
  };

  const handleAction = () => {
    if (!target) return;
    if (target.action === 'open_chat') {
      navigate(target.route);
    } else {
      if (onSelectStep && target.step_id) {
        onSelectStep(target.step_id);
      } else {
        const route = target.route.includes('stepId=')
          ? target.route
          : `${target.route}?stepId=${encodeURIComponent(target.step_id)}`;
        navigate(route);
      }
    }
  };

  const waitingDuration = formatWaitingDuration(
    waiting_step?.waiting_duration_seconds
  );

  const renderHelpAction = () => {
    if (!help_action) return null;
    if (!help_action.available) {
      return (
        <div className={styles.noHelpNotice} data-testid="no-help-notice">
          <span>{help_action.label}</span>
          <span className={styles.subtext}>
            В вашей команде пока нет активного HR-специалиста. Обратитесь к наставнику.
          </span>
        </div>
      );
    }

    const isInternal = help_action.url.startsWith('/');
    if (isInternal) {
      return (
        <Link
          to={help_action.url}
          className={styles.helpButton}
          data-testid="help-action-btn"
        >
          {help_action.label}
        </Link>
      );
    }

    return (
      <a
        href={help_action.url}
        className={styles.helpButton}
        target="_blank"
        rel="noopener noreferrer"
        data-testid="help-action-btn"
      >
        {help_action.label}
      </a>
    );
  };

  return (
    <aside
      className={styles.card}
      aria-label="Рекомендованный следующий шаг"
      data-state={state}
      data-testid="recommendation-card"
    >
      <div className={styles.cardHeader}>
        <h2 className={styles.cardHeading}>Рекомендация к выполнению</h2>
        <div className={styles.badgeRow}>
          <StatusBadge
            status={badgeConfig.status}
            label={badgeConfig.label}
            size="sm"
          />
          {period && (
            <span className={styles.periodBadge} data-testid="recommendation-period">
              {period}
            </span>
          )}
        </div>
      </div>

      <div className={styles.mainContent}>
        {state === 'completed' ? (
          <div className={styles.completedContent}>
            <p className={styles.completedTitle}>Все задачи выполнены 🎉</p>
            <p className={styles.completedText}>{reason}</p>
          </div>
        ) : state === 'empty_plan' ? (
          <div className={styles.completedContent}>
            <p className={styles.completedTitle}>План адаптации формируется</p>
            <p className={styles.completedText}>{reason}</p>
          </div>
        ) : (
          <>
            {title && (
              <div className={styles.stepTitle} data-testid="recommendation-title">
                Шаг: «{title}»
              </div>
            )}
            <p className={styles.reason} data-testid="recommendation-reason">
              {reason}
            </p>
          </>
        )}

        {waiting_step && (
          <div className={styles.waitingNotice} data-testid="waiting-step-notice">
            <div className={styles.waitingHeader}>
              <span className={styles.waitingDot} aria-hidden="true" />
              <strong>Приоритетный шаг:</strong>
              <span>«{waiting_step.title}»</span>
              {waiting_step.period && (
                <span className={styles.periodText}>({waiting_step.period})</span>
              )}
            </div>
            <p className={styles.waitingReason}>{waiting_step.reason}</p>
            {waitingDuration && (
              <span className={styles.waitingDuration} data-testid="waiting-duration">
                В ожидании: {waitingDuration}
              </span>
            )}
            {onSelectStep && (
              <button
                type="button"
                className={styles.viewWaitingBtn}
                onClick={() => onSelectStep(waiting_step.step_id)}
                aria-label={`Перейти к шагу ${waiting_step.title}`}
              >
                Посмотреть шаг
              </button>
            )}
          </div>
        )}

        <div className={styles.actionsRow}>
          {action_label && target && (
            <button
              type="button"
              className={styles.actionButton}
              onClick={handleAction}
              aria-label={`Рекомендация: ${action_label}`}
              data-testid="recommendation-action-btn"
            >
              {action_label}
            </button>
          )}

          {renderHelpAction()}
        </div>
      </div>
    </aside>
  );
};
