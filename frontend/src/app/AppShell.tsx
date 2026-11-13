import React from 'react';
import { NavLink, Outlet, Link } from 'react-router-dom';
import clsx from 'clsx';
import { AvitoLogo, Button } from '@/shared/ui';
import { roleHome, roleLabel, useAccessScopeRefresh, useLogout, useMe } from '@/features/session';
import styles from './AppShell.module.css';

const navigationByRole = {
 intern: [{ to: '/onboarding', text: 'Мой план' }, { to: '/chat', text: 'Чат' }],
 mentor: [{ to: '/mentor/questions', text: 'Вопросы' }, { to: '/mentor/documents', text: 'Знания' }, { to: '/mentor/interns', text: 'Стажёры' }],
 hr: [{ to: '/hr/questions', text: 'Обращения' }, { to: '/hr/documents', text: 'Инструкции' }],
};

export const AppShell: React.FC = () => {
  const me = useMe();
  const logout = useLogout();
  const user = me.data;
  const scope = useAccessScopeRefresh(user?.id);
  const home = user ? roleHome[user.role] : '/login';
  const navigation = user ? navigationByRole[user.role] : [];

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
      <header className={clsx(styles.header, user?.role !== 'intern' && styles.mentorHeader)}>
        <div className={styles.headerInner}>
          <div className={styles.brandSection}>
            <Link to={home} className={styles.logoLink} aria-label={user?.role === 'hr' ? 'AI-buddy — На главную (кадровые обращения)' : user?.role === 'mentor' ? 'AI-buddy — На главную (вопросы наставнику)' : 'AI-buddy — На главную (план адаптации)'}>
              <AvitoLogo size="md" />
              <span className={styles.buddyTag}>AI-buddy</span>
            </Link>
          </div>

          <nav className={styles.navigation} aria-label="Основная навигация">
            {navigation.map(item => <NavLink key={item.to}
              to={item.to}
              className={({ isActive }) =>
                clsx(styles.navLink, isActive && styles.activeLink)
              }
            >
              {item.text}
            </NavLink>)}
          </nav>

          {user && (
            <div className={styles.userSection}>
              <div className={styles.userAvatarChip} title={user.name}>
                {initials}
              </div>
              <div className={styles.userInfo}>
                <span className={styles.userName}>{user.name}</span>
                <span className={styles.userRole}>
                  {roleLabel[user.role]}, {user.team.name}
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
        {scope === 'denied' ? <p role="alert">Область доступа недоступна.</p> : <Outlet key={String(user?.id) + ":" + scope} />}
      </main>
    </div>
  );
};
