import React from 'react';
import clsx from 'clsx';
import styles from './StatusBadge.module.css';

export type BadgeStatus =
  // Onboarding step statuses
  | 'not_started'
  | 'in_progress'
  | 'blocked'
  | 'completed'
  // Access request statuses
  | 'draft'
  | 'submitting'
  | 'pending'
  | 'granted'
  | 'rejected'
  | 'failed';

export interface StatusBadgeProps {
  status: BadgeStatus;
  label?: string; // Optional custom label override
  className?: string;
  size?: 'sm' | 'md';
}

const STATUS_CONFIG: Record<
  BadgeStatus,
  { label: string; tone: 'neutral' | 'info' | 'warning' | 'success' | 'danger' }
> = {
  // Plan steps
  not_started: { label: 'Не начат', tone: 'neutral' },
  in_progress: { label: 'В работе', tone: 'info' },
  blocked: { label: 'Заблокирован', tone: 'neutral' },
  completed: { label: 'Выполнен', tone: 'success' },

  // Requests
  draft: { label: 'Черновик', tone: 'neutral' },
  submitting: { label: 'Отправка...', tone: 'info' },
  pending: { label: 'Ожидает согласования', tone: 'warning' },
  granted: { label: 'Доступ выдан', tone: 'success' },
  rejected: { label: 'Отклонён', tone: 'danger' },
  failed: { label: 'Ошибка', tone: 'danger' },
};

export const StatusBadge: React.FC<StatusBadgeProps> = ({
  status,
  label,
  className,
  size = 'md',
}) => {
  const config = STATUS_CONFIG[status] || { label: status, tone: 'neutral' };
  const displayText = label || config.label;

  return (
    <span
      className={clsx(
        styles.badge,
        styles[config.tone],
        styles[size],
        className
      )}
      role="status"
    >
      <span className={styles.dot} aria-hidden="true" />
      <span className={styles.label}>{displayText}</span>
    </span>
  );
};
