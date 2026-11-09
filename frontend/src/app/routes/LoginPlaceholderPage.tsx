import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, StatusBadge } from '@/shared/ui';
import styles from './LoginPlaceholderPage.module.css';

export const LoginPlaceholderPage: React.FC = () => {
  const [selectedUser, setSelectedUser] = useState<'intern' | 'mentor'>('intern');

  return (
    <div className={styles.loginContainer}>
      <div className={styles.loginCard}>
        <div className={styles.logoRow}>
          <div className={styles.logoDots} aria-hidden="true">
            <span className={`${styles.dot} ${styles.dotGreen}`} />
            <span className={`${styles.dot} ${styles.dotBlue}`} />
            <span className={`${styles.dot} ${styles.dotRed}`} />
            <span className={`${styles.dot} ${styles.dotViolet}`} />
          </div>
          <span className={styles.brandTitle}>Авито</span>
        </div>

        <h1 className={styles.title}>Тестовый вход в демо-режиме</h1>
        <p className={styles.subtitle}>
          Выберите синтетический аккаунт для проверки сценариев адаптации стажёра и согласования доступов.
        </p>

        <div className={styles.accountsList}>
          {/* Intern Account */}
          <div
            className={`${styles.accountCard} ${selectedUser === 'intern' ? styles.accountCardActive : ''}`}
            onClick={() => setSelectedUser('intern')}
            role="button"
            tabIndex={0}
          >
            <div className={styles.accountAvatar}>ИС</div>
            <div className={styles.accountDetails}>
              <span className={styles.accountName}>Иван Стажёров</span>
              <span className={styles.accountRole}>Стажёр Backend • team_demo_backend</span>
            </div>
            <StatusBadge status="in_progress" label="Активен" size="sm" />
          </div>

          {/* Mentor Account */}
          <div
            className={`${styles.accountCard} ${selectedUser === 'mentor' ? styles.accountCardActive : ''}`}
            onClick={() => setSelectedUser('mentor')}
            role="button"
            tabIndex={0}
          >
            <div
              className={styles.accountAvatar}
              style={{ backgroundColor: 'var(--avito-violet)' }}
            >
              АМ
            </div>
            <div className={styles.accountDetails}>
              <span className={styles.accountName}>Алексей Менторов</span>
              <span className={styles.accountRole}>Наставник / Team Lead</span>
            </div>
            <StatusBadge status="completed" label="Ментор" size="sm" />
          </div>
        </div>

        <div style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <Link to="/onboarding" style={{ width: '100%' }}>
            <Button variant="primary" fullWidth>
              Войти как {selectedUser === 'intern' ? 'Иван (Стажёр)' : 'Алексей (Ментор)'}
            </Button>
          </Link>

          <Link to="/onboarding" style={{ width: '100%' }}>
            <Button variant="secondary" fullWidth>
              Вернуться в систему
            </Button>
          </Link>
        </div>

        <p className={styles.noticeNote}>
          Сессия сохраняется в защищенной <code>HttpOnly</code> cookie (задача #4). В рабочей среде
          будет использоваться корпоративная SSO-авторизация.
        </p>
      </div>
    </div>
  );
};
