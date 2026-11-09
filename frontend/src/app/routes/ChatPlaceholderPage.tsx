import React from 'react';
import { useParams, useSearchParams, Link } from 'react-router-dom';
import { StatusBadge } from '@/shared/ui';
import styles from './ChatPlaceholderPage.module.css';

export const ChatPlaceholderPage: React.FC = () => {
  const { conversationId } = useParams<{ conversationId?: string }>();
  const [searchParams] = useSearchParams();
  const stepId = searchParams.get('stepId');

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <div className={styles.titleRow}>
          <h1 className={styles.title}>Чат с наставником</h1>
          {conversationId ? (
            <StatusBadge status="in_progress" label={`Диалог: ${conversationId}`} />
          ) : (
            <StatusBadge status="draft" label="Новый диалог" />
          )}
        </div>

        {stepId && (
          <div className={styles.stepBanner}>
            <span className={styles.stepLabel}>Контекст шага адаптации:</span>
            <code className={styles.stepCode}>{stepId}</code>
            <Link to="/onboarding" className={styles.backLink}>
              ← Вернуться к плану
            </Link>
          </div>
        )}
      </div>

      <div className={styles.contentCard}>
        <div className={styles.slotIllustration}>
          <div className={styles.avatarBubble}>
            <svg
              className={styles.avatarIcon}
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path d="M12 2a8 8 0 00-8 8c0 3.5 2.5 6.5 6 7.5V20l3-2 3 2v-2.5c3.5-1 6-4 6-7.5a8 8 0 00-8-8z" />
            </svg>
          </div>
        </div>

        <h2 className={styles.cardHeading}>Слот диалога готов к интеграции</h2>
        <p className={styles.cardText}>
          Каркас маршрутов <code>/chat</code> и <code>/chat/:conversationId</code> развернут.
          Никита (FE1) подключает сюда API-клиент, историю сообщений, SSE-стриминг DeepSeek и
          карточку заявки доступа.
        </p>

        <div className={styles.featuresList}>
          <div className={styles.featureItem}>
            <span className={styles.featureDot} />
            <span>Генерация типов из <code>contracts/openapi.yaml</code> готова (<code>npm run generate:api</code>)</span>
          </div>
          <div className={styles.featureItem}>
            <span className={styles.featureDot} />
            <span>UI-компоненты доступны в <code>@/shared/ui</code> (Button, Drawer, StatusBadge, Spinner)</span>
          </div>
          <div className={styles.featureItem}>
            <span className={styles.featureDot} />
            <span>Проксирование API на <code>http://localhost:8080/api</code> активно в dev-режиме</span>
          </div>
        </div>
      </div>
    </div>
  );
};
