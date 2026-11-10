import React, { useState } from 'react';
import { Navigate } from 'react-router-dom';
import { AvitoLogo, Button, InlineError, Spinner, StatusBadge } from '@/shared/ui';
import type { User } from '@/shared/api';
import { useDemoAccounts, useLogin, useMe } from './hooks';
import styles from './LoginPage.module.css';

const ROLE_LABEL: Record<User['role'], string> = {
  intern: 'Стажёр',
  mentor: 'Наставник',
};

function initialsOf(name: string): string {
  const parts = name.split(/\s+/).filter(Boolean);
  return parts.map((part) => part[0]).join('').slice(0, 2).toUpperCase();
}

export const LoginPage: React.FC = () => {
  const me = useMe();
  const accounts = useDemoAccounts();
  const login = useLogin();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  if (me.data) {
    return <Navigate to="/onboarding" replace />;
  }

  const list = accounts.data ?? [];
  const selected = list.find((account) => account.id === selectedId) ?? list[0];

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
            {accounts.data ? `${accounts.data.length} роли доступны без регистрации` : 'Загружаем тестовые учетные записи'}
          </div>
        </section>

        <div className={styles.loginCard}>
          {accounts.isPending && (
            <div className={styles.accountsLoading} role="status" aria-label="Загрузка тестовых учетных записей">
              <Spinner size="lg" />
            </div>
          )}

          {accounts.isError && (
            <InlineError
              message={accounts.error instanceof Error ? accounts.error.message : 'Не удалось загрузить учетные записи'}
              onRetry={() => void accounts.refetch()}
              isRetrying={accounts.isRefetching}
            />
          )}

          {accounts.data && (
            <>
              <div className={styles.accountsList} role="radiogroup" aria-label="Тестовые учетные записи">
                {list.map((account) => {
                  const isActive = account.id === selected?.id;
                  return (
                    <button
                      key={account.id}
                      type="button"
                      role="radio"
                      aria-checked={isActive}
                      className={`${styles.accountCard} ${isActive ? styles.accountCardActive : ''}`}
                      onClick={() => setSelectedId(account.id)}
                    >
                      <span className={styles.accountAvatar} aria-hidden="true">
                        {initialsOf(account.name)}
                      </span>
                      <span className={styles.accountDetails}>
                        <span className={styles.accountName}>{account.name}</span>
                        <span className={styles.accountRole}>
                          {ROLE_LABEL[account.role]}, {account.team.name}
                        </span>
                      </span>
                      <StatusBadge
                        status={account.role === 'mentor' ? 'completed' : 'in_progress'}
                        label={ROLE_LABEL[account.role]}
                        size="sm"
                      />
                    </button>
                  );
                })}
              </div>

              {login.isError && (
                <InlineError
                  className={styles.loginError ?? ''}
                  message={login.error instanceof Error ? login.error.message : 'Не удалось войти'}
                />
              )}

              <div className={styles.actions}>
                <Button
                  variant="primary"
                  size="lg"
                  fullWidth
                  isLoading={login.isPending}
                  disabled={!selected}
                  onClick={() => selected && login.mutate(selected.id)}
                  rightIcon={<span className={styles.buttonArrow} aria-hidden="true">→</span>}
                >
                  {selected ? `Войти как ${selected.name}` : 'Выберите учетную запись'}
                </Button>
              </div>
            </>
          )}

          <p className={styles.noticeNote}>
            В демо-режиме переключение учетной записи происходит мгновенно. В рабочем контуре Авито авторизация выполняется через корпоративный SSO.
          </p>
        </div>
      </div>
    </div>
  );
};
