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

        <div className={styles.accountsList} role="radiogroup" aria-label="Тестовые учетные записи">
          {/* Intern Account */}
          <div
            className={`${styles.accountCard} ${selectedUser === 'intern' ? styles.accountCardActive : ''}`}
            onClick={() => setSelectedUser('intern')}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                setSelectedUser('intern');
              }
            }}
            role="radio"
            aria-checked={selectedUser === 'intern'}
            tabIndex={0}
          >
            <div className={styles.accountAvatar}>ИС</div>
            <div className={styles.accountDetails}>
              <span className={styles.accountName}>Иван Стажёров</span>
              <span className={styles.accountRole}>Стажёр Backend, команда Backend Core</span>
            </div>
            <StatusBadge status="in_progress" label="Активен" size="sm" />
          </div>

          {/* Mentor Account */}
          <div
            className={`${styles.accountCard} ${selectedUser === 'mentor' ? styles.accountCardActive : ''}`}
            onClick={() => setSelectedUser('mentor')}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                setSelectedUser('mentor');
              }
            }}
            role="radio"
            aria-checked={selectedUser === 'mentor'}
            tabIndex={0}
          >
            <div
              className={styles.accountAvatar}
              style={{ backgroundColor: 'var(--avito-violet)' }}
            >
              АМ
            </div>
            <div className={styles.accountDetails}>
              <span className={styles.accountName}>Алексей Морозов</span>
              <span className={styles.accountRole}>Lead Backend Engineer, наставник команды</span>
            </div>
            <StatusBadge status="completed" label="Наставник" size="sm" />
          </div>
        </div>

        <div style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <Link to="/onboarding" style={{ width: '100%' }}>
            <Button variant="primary" fullWidth>
              Войти как {selectedUser === 'intern' ? 'Иван Стажёров' : 'Алексей Морозов'}
            </Button>
          </Link>

          <Link to="/onboarding" style={{ width: '100%' }}>
            <Button variant="secondary" fullWidth>
              Вернуться в систему
            </Button>
          </Link>
        </div>

        <p className={styles.noticeNote}>
          В демо-режиме переключение учетной записи происходит мгновенно. В рабочем контуре Авито авторизация выполняется через корпоративный SSO.
        </p>
      </div>
    </div>
  );
};
