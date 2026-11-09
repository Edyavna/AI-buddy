import React from 'react';
import { Link } from 'react-router-dom';
import { Button, StatusBadge } from '@/shared/ui';
import styles from './ChatPlaceholderPage.module.css';

export const OnboardingPlaceholderPage: React.FC = () => {
  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <div className={styles.titleRow}>
          <h1 className={styles.title}>Мой план адаптации</h1>
          <StatusBadge status="in_progress" label="Прогресс: 0%" />
        </div>
      </div>

      <div className={styles.contentCard}>
        <h2 className={styles.cardHeading}>План адаптации стажёра</h2>
        <p className={styles.cardText}>
          Экран плана (задача #4) отобразит последовательность шагов, статус доступности,
          прогресс и кнопку «Спросить помощника» для перехода в чат с контекстом шага.
        </p>

        <div style={{ marginTop: '16px' }}>
          <Link to="/chat?stepId=step_local_setup">
            <Button variant="primary">
              Перейти в чат по первому шагу (step_local_setup)
            </Button>
          </Link>
        </div>
      </div>
    </div>
  );
};
