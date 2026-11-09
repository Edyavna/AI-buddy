import React from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@/shared/ui';
import styles from './ChatPlaceholderPage.module.css';

export const LoginPlaceholderPage: React.FC = () => {
  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1 className={styles.title}>Тестовый вход в демо-режиме</h1>
      </div>

      <div className={styles.contentCard}>
        <h2 className={styles.cardHeading}>Демо-аутентификация стажёра</h2>
        <p className={styles.cardText}>
          Экран выбора учетных записей (задача #4) загрузит список синтетических аккаунтов из{' '}
          <code>/api/v1/demo/accounts</code> и позволит переключать сессии через HttpOnly куки.
        </p>

        <div style={{ marginTop: '16px' }}>
          <Link to="/onboarding">
            <Button variant="secondary">Вернуться в систему</Button>
          </Link>
        </div>
      </div>
    </div>
  );
};
