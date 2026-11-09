import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, StatusBadge, AvitoLogo } from '@/shared/ui';
import styles from './LoginPlaceholderPage.module.css';

export const LoginPlaceholderPage: React.FC = () => {
  const [selectedUser, setSelectedUser] = useState<'intern' | 'mentor'>('intern');

  return (
    <div className={styles.loginContainer}>
      <div className={styles.loginCard}>
        <div className={styles.logoRow}>
          <AvitoLogo size="lg" />
        </div>

        <h1 className={styles.title}>Тестовый вход в демо-режиме</h1>
        <p className={styles.subtitle}>
          Выберите тестовую учетную запись для проверки сценариев адаптации и согласования доступов.
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
              <span className={styles.accountName}>Иван Стажеров</span>
              <span className={styles.accountRole}>Стажер Backend, team_demo_backend</span>
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
              <span className={styles.accountRole}>Наставник команды</span>
            </div>
            <StatusBadge status="completed" label="Ментор" size="sm" />
          </div>
        </div>

        <div style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <Link to="/onboarding" style={{ width: '100%' }}>
            <Button variant="primary" fullWidth>
              Войти как {selectedUser === 'intern' ? 'Иван' : 'Алексей'}
            </Button>
          </Link>

          <Link to="/onboarding" style={{ width: '100%' }}>
            <Button variant="secondary" fullWidth>
              Вернуться в систему
            </Button>
          </Link>
        </div>

        <p className={styles.noticeNote}>
          Сессия сохраняется в <code>HttpOnly</code> cookie. В рабочей среде используется корпоративная авторизация.
        </p>
      </div>
    </div>
  );
};
