import React from 'react';
import { NavLink, Outlet, Link } from 'react-router-dom';
import clsx from 'clsx';
import styles from './AppShell.module.css';

export interface AppShellProps {
  userName?: string;
  userRole?: string;
}

export const AppShell: React.FC<AppShellProps> = ({
  userName = 'Иван Стажёров',
  userRole = 'Стажёр Backend',
}) => {
  return (
    <div className={styles.layout}>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <div className={styles.brandSection}>
            <Link to="/onboarding" className={styles.logoLink} aria-label="AI-buddy — Главная">
              {/* Iconic Avito 4 colored dots logo */}
              <div className={styles.logoDots} aria-hidden="true">
                <span className={clsx(styles.dot, styles.dotGreen)} />
                <span className={clsx(styles.dot, styles.dotBlue)} />
                <span className={clsx(styles.dot, styles.dotRed)} />
                <span className={clsx(styles.dot, styles.dotViolet)} />
              </div>
              <div className={styles.brandText}>
                <span className={styles.brandName}>Авито</span>
                <span className={styles.buddyTag}>AI-buddy</span>
              </div>
            </Link>
          </div>

          <nav className={styles.navigation} aria-label="Основная навигация">
            <NavLink
              to="/onboarding"
              className={({ isActive }) =>
                clsx(styles.navLink, { [styles.activeLink]: isActive })
              }
            >
              <svg
                className={styles.navIcon}
                viewBox="0 0 20 20"
                fill="currentColor"
                aria-hidden="true"
              >
                <path d="M10 2a1 1 0 011 1v1.323l3.954 1.582 1.599-.8a1 1 0 01.894 1.79l-1.233.616 1.738 5.42a1 1 0 01-.285 1.05A3.989 3.989 0 0115 15a3.989 3.989 0 01-2.667-1.019 1 1 0 01-.285-1.05l1.715-5.349L11 6.477V16h2a1 1 0 110 2H7a1 1 0 110-2h2V6.477L6.237 7.582l1.715 5.349a1 1 0 01-.285 1.05A3.989 3.989 0 015 15a3.989 3.989 0 01-2.667-1.019 1 1 0 01-.285-1.05l1.738-5.42-1.233-.617a1 1 0 01.894-1.788l1.599.799L9 4.323V3a1 1 0 011-1z" />
              </svg>
              <span>Мой план</span>
            </NavLink>

            <NavLink
              to="/chat"
              className={({ isActive }) =>
                clsx(styles.navLink, { [styles.activeLink]: isActive })
              }
            >
              <svg
                className={styles.navIcon}
                viewBox="0 0 20 20"
                fill="currentColor"
                aria-hidden="true"
              >
                <path
                  fillRule="evenodd"
                  d="M10 2c-4.418 0-8 3.134-8 7 0 1.76.73 3.376 1.95 4.634a.75.75 0 01.21.618l-.513 2.565a.75.75 0 001.026.837l2.846-1.423a.75.75 0 01.554-.055A8.45 8.45 0 0010 16c4.418 0 8-3.134 8-7s-3.582-7-8-7zm-4 7a1 1 0 112 0 1 1 0 01-2 0zm4 1a1 1 0 100-2 1 1 0 000 2zm5-1a1 1 0 11-2 0 1 1 0 012 0z"
                  clipRule="evenodd"
                />
              </svg>
              <span>Чат с наставником</span>
            </NavLink>
          </nav>

          <div className={styles.userSection}>
            <div className={styles.userInfo}>
              <span className={styles.userName}>{userName}</span>
              <span className={styles.userRole}>{userRole}</span>
            </div>
            <Link to="/login" className={styles.switchAccountButton} title="Сменить аккаунт демо">
              Сменить
            </Link>
          </div>
        </div>
      </header>

      <main className={styles.mainContent}>
        <Outlet />
      </main>
    </div>
  );
};
