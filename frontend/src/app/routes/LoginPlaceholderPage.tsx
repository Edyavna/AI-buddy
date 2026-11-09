import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, StatusBadge, AvitoLogo } from '@/shared/ui';
import styles from './LoginPlaceholderPage.module.css';

export const LoginPlaceholderPage: React.FC = () => {
  const [selectedUser, setSelectedUser] = useState<'intern' | 'mentor'>('intern');

  return (
    <div className={styles.loginContainer}>
      <div className={styles.loginLayout}>
        <section className={styles.loginIntro}>
          <div className={styles.logoRow}>
            <AvitoLogo size="lg" />
            <span className={styles.productName}>AI-buddy</span>
          </div>

          <h1 className={styles.title}>Тестовый вход в демо-режиме</h1>
          <p className={styles.subtitle}>
            Выберите тестовую учетную запись для проверки сценариев адаптации и согласования доступов.
          </p>
          <div className={styles.demoFact}>
            <span className={styles.demoFactDot} aria-hidden="true" />
            2 роли доступны без регистрации
          </div>
        </section>

        <div className={styles.loginCard}>
          <div className={styles.accountsList} role="radiogroup" aria-label="Тестовые учетные записи">
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
              <div className={`${styles.accountAvatar} ${styles.accountAvatarMentor}`}>
                АМ
              </div>
              <div className={styles.accountDetails}>
                <span className={styles.accountName}>Алексей Морозов</span>
                <span className={styles.accountRole}>Lead Backend Engineer, наставник команды</span>
              </div>
              <StatusBadge status="completed" label="Наставник" size="sm" />
            </div>
          </div>

          <div className={styles.actions}>
            <Link to="/onboarding" className={styles.actionLink}>
              <Button
                variant="primary"
                size="lg"
                fullWidth
                rightIcon={<span className={styles.buttonArrow} aria-hidden="true">→</span>}
              >
                Войти как {selectedUser === 'intern' ? 'Иван Стажёров' : 'Алексей Морозов'}
              </Button>
            </Link>

            <Link to="/onboarding" className={styles.actionLink}>
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
    </div>
  );
};
