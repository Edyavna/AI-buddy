import React from 'react';
import { Link } from 'react-router-dom';
import { Button, StatusBadge } from '@/shared/ui';
import styles from './OnboardingPlaceholderPage.module.css';

export const OnboardingPlaceholderPage: React.FC = () => {
  return (
    <div className={styles.container}>
      {/* Banner */}
      <section className={styles.heroBanner}>
        <div className={styles.heroInfo}>
          <div className={styles.heroTag}>
            <span>Программа адаптации</span>
          </div>
          <h1 className={styles.heroTitle}>Мой план адаптации</h1>
          <p className={styles.heroSubtitle}>
            План начальной адаптации стажера в команде Backend Core. Выполняйте шаги последовательно
            и запрашивайте доступ к сервисам через ассистента.
          </p>
        </div>

        <div className={styles.heroProgressCard}>
          <div className={styles.progressLabelRow}>
            <span className={styles.progressLabel}>Текущий прогресс</span>
            <StatusBadge status="in_progress" label="Прогресс: 0%" size="sm" />
          </div>
          <div
            className={styles.progressBarTrack}
            role="progressbar"
            aria-valuenow={0}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div className={styles.progressBarFill} style={{ width: '4%' }} />
          </div>
          <span className={styles.progressMeta}>Завершено 0 из 4 шагов. Следующий шаг доступен.</span>
        </div>
      </section>

      {/* Main Content Layout */}
      <div className={styles.layoutGrid}>
        {/* Steps List */}
        <div className={styles.stepsSection}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>Шаги онбординга</h2>
            <span className={styles.progressMeta}>Команда Backend Core</span>
          </div>

          <div className={styles.stepsList}>
            {/* Step 1: Active */}
            <article className={`${styles.stepCard} ${styles.stepCardActive}`}>
              <div className={styles.stepCardHeader}>
                <div className={styles.stepTitleGroup}>
                  <span className={styles.stepNumber}>1</span>
                  <div>
                    <h3 className={styles.stepTitle}>Настройка локального окружения и репозитория</h3>
                  </div>
                </div>
                <StatusBadge status="in_progress" />
              </div>

              <p className={styles.stepDescription}>
                Склонировать репозиторий, запустить зависимости через Docker Compose и проверить
                прохождение локальных тестов. Для доступа к репозиторию требуется подтвердить заявку на чтение.
              </p>

              <div className={styles.stepFooter}>
                <div className={styles.stepMetaGroup}>
                  <span className={styles.resourceTag}>repo_demo_backend</span>
                  <span className={styles.progressMeta}>Шаг step_local_setup</span>
                </div>
                <Link to="/chat?stepId=step_local_setup">
                  <Button variant="primary" size="sm">
                    Перейти в чат по первому шагу (step_local_setup)
                  </Button>
                </Link>
              </div>
            </article>

            {/* Step 2: Blocked */}
            <article className={styles.stepCard}>
              <div className={styles.stepCardHeader}>
                <div className={styles.stepTitleGroup}>
                  <span className={styles.stepNumber}>2</span>
                  <h3 className={styles.stepTitle}>Знакомство с архитектурой и контрактами сервиса</h3>
                </div>
                <StatusBadge status="blocked" />
              </div>

              <p className={styles.stepDescription}>
                Изучить спецификацию OpenAPI, формат событий SSE и архитектуру сервисов.
              </p>

              <div className={styles.stepFooter}>
                <span className={styles.blockedNotice}>Ожидает завершения шага 1</span>
                <Link to="/chat?stepId=step_architecture">
                  <Button variant="ghost" size="sm">
                    Спросить наставника
                  </Button>
                </Link>
              </div>
            </article>

            {/* Step 3: Blocked */}
            <article className={styles.stepCard}>
              <div className={styles.stepCardHeader}>
                <div className={styles.stepTitleGroup}>
                  <span className={styles.stepNumber}>3</span>
                  <h3 className={styles.stepTitle}>Первый Pull Request в тестовый контур</h3>
                </div>
                <StatusBadge status="blocked" />
              </div>

              <p className={styles.stepDescription}>
                Внести исправление в сервис задач, запустить линтер и отправить PR на ревью.
              </p>

              <div className={styles.stepFooter}>
                <span className={styles.blockedNotice}>Ожидает завершения шага 2</span>
              </div>
            </article>

            {/* Step 4: Blocked */}
            <article className={styles.stepCard}>
              <div className={styles.stepCardHeader}>
                <div className={styles.stepTitleGroup}>
                  <span className={styles.stepNumber}>4</span>
                  <h3 className={styles.stepTitle}>Встреча 1-on-1 и итоги первой недели</h3>
                </div>
                <StatusBadge status="blocked" />
              </div>

              <p className={styles.stepDescription}>
                Обсудить с наставником результаты первой недели и план на испытательный срок.
              </p>
            </article>
          </div>
        </div>

        {/* Sidebar Widgets */}
        <aside className={styles.sidebar}>
          {/* Mentor card */}
          <div className={styles.widgetCard}>
            <h3 className={styles.widgetTitle}>Наставник</h3>
            <div className={styles.mentorRow}>
              <div className={styles.mentorAvatar}>АМ</div>
              <div className={styles.mentorDetails}>
                <span className={styles.mentorName}>Алексей Морозов</span>
                <span className={styles.mentorRole}>Lead Backend Engineer</span>
              </div>
            </div>
            <Link to="/chat">
              <Button variant="secondary" size="sm" fullWidth>
                Открыть чат
              </Button>
            </Link>
          </div>

          {/* Useful links */}
          <div className={styles.widgetCard}>
            <h3 className={styles.widgetTitle}>База знаний и регламенты</h3>
            <nav className={styles.linksList}>
              <a
                href="#docs"
                className={styles.quickLink}
                onClick={(e) => {
                  e.preventDefault();
                  alert('База знаний стажеров');
                }}
              >
                <span>Вики команды Backend</span>
                <span>→</span>
              </a>
              <a
                href="#git"
                className={styles.quickLink}
                onClick={(e) => {
                  e.preventDefault();
                  alert('Репозитории проектов команды');
                }}
              >
                <span>Регламент Git и CI/CD</span>
                <span>→</span>
              </a>
              <a
                href="#access"
                className={styles.quickLink}
                onClick={(e) => {
                  e.preventDefault();
                  alert('Сервис заявок на доступ');
                }}
              >
                <span>Сервис доступов</span>
                <span>→</span>
              </a>
            </nav>
          </div>
        </aside>
      </div>
    </div>
  );
};
