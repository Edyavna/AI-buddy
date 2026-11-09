import React from 'react';
import clsx from 'clsx';
import { Button } from '../Button/Button';
import styles from './InlineError.module.css';

export interface InlineErrorProps {
  message: string;
  onRetry?: () => void;
  className?: string;
  isRetrying?: boolean;
}

export const InlineError: React.FC<InlineErrorProps> = ({
  message,
  onRetry,
  className,
  isRetrying = false,
}) => {
  return (
    <div
      role="alert"
      aria-live="assertive"
      className={clsx(styles.container, className)}
    >
      <div className={styles.iconWrapper} aria-hidden="true">
        <svg
          className={styles.icon}
          width="20"
          height="20"
          viewBox="0 0 20 20"
          fill="currentColor"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path
            fillRule="evenodd"
            d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.28 7.22a.75.75 0 00-1.06 1.06L8.94 10l-1.72 1.72a.75.75 0 101.06 1.06L10 11.06l1.72 1.72a.75.75 0 101.06-1.06L11.06 10l1.72-1.72a.75.75 0 00-1.06-1.06L10 8.94 8.28 7.22z"
            clipRule="evenodd"
          />
        </svg>
      </div>

      <div className={styles.content}>
        <p className={styles.message}>{message}</p>
      </div>

      {onRetry && (
        <div className={styles.action}>
          <Button
            size="sm"
            variant="secondary"
            onClick={onRetry}
            isLoading={isRetrying}
          >
            Повторить
          </Button>
        </div>
      )}
    </div>
  );
};
