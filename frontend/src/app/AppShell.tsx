import React from 'react';
import { NavLink, Outlet, Link } from 'react-router-dom';
import clsx from 'clsx';
import { AvitoLogo, Button } from '@/shared/ui';
import { useLogout, useMe } from '@/features/session';
import styles from './AppShell.module.css';

const ROLE_LABEL = { intern: 'Стажёр', mentor: 'Наставник' } as const;

export const AppShell: React.FC = () => {
  const me = useMe();
  const logout = useLogout();
  const user = me.data;

  const initials = (user?.name ?? '')
    .split(/\s+/)
    .filter(Boolean)
    .map((part) => part[0])
    .join('')
    .slice(0, 2)
    .toUpperCase();

  return (
    <div className={styles.layout}>
      <a className={styles.skipLink} href="#main-content">
        К содержимому
      </a>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <div className={styles.brandSection}>
            <Link to="/onboarding" className={styles.logoLink} aria-label="AI-buddy — На главную (план адаптации)">
              <AvitoLogo size="md" />
              <span className={styles.buddyTag}>AI-buddy</span>
            </Link>
          </div>

          <nav className={styles.navigation} aria-label="Основная навигация">
            <NavLink
              to="/onboarding"
              className={({ isActive }) =>
                clsx(styles.navLink, isActive && styles.activeLink)
              }
            >
              Мой план
            </NavLink>
            <NavLink
              to="/chat"
              className={({ isActive }) =>
                clsx(styles.navLink, isActive && styles.activeLink)
              }
            >
              Чат
            </NavLink>
          </nav>

          {user && (
            <div className={styles.userSection}>
              <div className={styles.userAvatarChip} title={user.name}>
                {initials}
              </div>
              <div className={styles.userInfo}>
                <span className={styles.userName}>{user.name}</span>
                <span className={styles.userRole}>
                  {ROLE_LABEL[user.role]}, {user.team.name}
                </span>
              </div>
              <Button
                variant="ghost"
                size="sm"
                className={styles.switchAccountButton}
                onClick={() => logout.mutate()}
                isLoading={logout.isPending}
              >
                Выйти
              </Button>
            </div>
          )}
        </div>
      </header>

      <main className={styles.mainContent} id="main-content">
        <Outlet />
      </main>
    </div>
  );
};
