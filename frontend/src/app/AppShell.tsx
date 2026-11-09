import React from 'react';
import { NavLink, Outlet, Link } from 'react-router-dom';
import clsx from 'clsx';
import { AvitoLogo } from '@/shared/ui';
import styles from './AppShell.module.css';

export interface AppShellProps {
  userName?: string;
  userRole?: string;
}

export const AppShell: React.FC<AppShellProps> = ({
  userName = 'Иван Стажёров',
  userRole = 'Стажёр Backend',
}) => {
  const initials = userName
    .split(' ')
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
                clsx(styles.navLink, { [styles.activeLink]: isActive })
              }
            >
              Мой план
            </NavLink>
            <NavLink
              to="/chat"
              className={({ isActive }) =>
                clsx(styles.navLink, { [styles.activeLink]: isActive })
              }
            >
              Чат
            </NavLink>
          </nav>

          <div className={styles.userSection}>
            <div className={styles.userAvatarChip} title={userName}>
              {initials}
            </div>
            <div className={styles.userInfo}>
              <span className={styles.userName}>{userName}</span>
              <span className={styles.userRole}>{userRole}</span>
            </div>
            <Link
              to="/login"
              className={styles.switchAccountButton}
              title="Сменить тестового пользователя"
              aria-label="Сменить тестового пользователя"
            >
              Сменить
            </Link>
          </div>
        </div>
      </header>

      <main className={styles.mainContent} id="main-content">
        <Outlet />
      </main>
    </div>
  );
};
