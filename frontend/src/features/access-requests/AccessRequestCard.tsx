import React from 'react';
import { Button, StatusBadge } from '@/shared/ui';
import type { AccessRequest } from './schemas';
import styles from './AccessRequestCard.module.css';

const PERMISSION_LABEL: Record<AccessRequest['resource']['permission'], string> = {
  read: 'чтение',
  write: 'запись',
};

const STATUS_HINT: Record<AccessRequest['status'], string | null> = {
  draft: null,
  submitting: 'Отправляем заявку / уточняем результат',
  pending: null,
  granted: null,
  rejected: null,
  failed: null,
};

function formatCheckedAt(value: string): string {
  return new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' }).format(new Date(value));
}

export interface AccessRequestCardProps {
  request: AccessRequest;
  isConfirming?: boolean;
  error?: string | null;
  // Последний успешный статус сохранён; фоновое обновление не удалось.
  refreshFailed?: boolean;
  onConfirm?: () => void;
  onDiscuss?: () => void;
}

export const AccessRequestCard: React.FC<AccessRequestCardProps> = ({
  request,
  isConfirming = false,
  error,
  refreshFailed = false,
  onConfirm,
  onDiscuss,
}) => {
  const hint = STATUS_HINT[request.status];
  const showFailure = request.status === 'rejected' || request.status === 'failed';
  // Подтверждение доступно только до отправки; failed — явный повтор по тому же requestId.
  const canConfirm = (request.status === 'draft' || request.status === 'failed') && Boolean(onConfirm);

  return (
    <article className={styles.card} data-request-id={request.id}>
      <div className={styles.header}>
        <h4 className={styles.resourceName}>{request.resource.name}</h4>
        <StatusBadge status={request.status} size="sm" />
      </div>

      <p className={styles.meta}>
        Право: {PERMISSION_LABEL[request.resource.permission]}
      </p>
      <p className={styles.reason}>{request.reason}</p>

      {hint && <p className={styles.hint}>{hint}</p>}
      {showFailure && (
        <p className={styles.failure}>
          {request.failure_reason ?? (request.status === 'rejected' ? 'Заявка отклонена' : 'Не удалось отправить заявку')}
        </p>
      )}
      {error && (
        <p className={styles.error} role="alert">{error}</p>
      )}
      {refreshFailed && (
        <p className={styles.refreshError} role="status">
          Не удалось обновить статус. Показано последнее известное состояние.
        </p>
      )}

      <div className={styles.footer}>
        {request.external_id && (
          <span className={styles.externalId}>Номер: {request.external_id}</span>
        )}
        {request.status_checked_at && (
          <span className={styles.checkedAt}>
            Проверено: {formatCheckedAt(request.status_checked_at)}
          </span>
        )}
        <span className={styles.actions}>
          {canConfirm && (
            <Button
              variant="primary"
              size="sm"
              onClick={onConfirm}
              isLoading={isConfirming}
              disabled={isConfirming}
            >
              {request.status === 'failed' ? 'Отправить повторно' : 'Подтвердить отправку'}
            </Button>
          )}
          {(request.status === 'rejected' || request.status === 'failed') && onDiscuss && (
            <Button variant="secondary" size="sm" onClick={onDiscuss}>
              Обсудить с помощником
            </Button>
          )}
        </span>
      </div>
    </article>
  );
};
