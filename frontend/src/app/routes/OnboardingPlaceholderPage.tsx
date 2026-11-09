import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, StatusBadge, Drawer } from '@/shared/ui';
import styles from './OnboardingPlaceholderPage.module.css';

interface DocArticle {
  title: string;
  content: React.ReactNode;
}

const DOCUMENTATION: Record<string, DocArticle> = {
  wiki: {
    title: 'Вики команды Backend Core',
    content: (
      <div className={styles.docContainer}>
        <p>
          Добро пожаловать в команду Backend Core! Мы разрабатываем ключевые сервисы взаимодействия
          пользователей и транзакций на платформе Авито.
        </p>
        <h4 className={styles.docSectionTitle}>Архитектура и стек</h4>
        <p>
          Сервисы пишутся на Go (версия 1.22+). Основное хранилище — PostgreSQL 16, очереди сообщений —
          Apache Kafka, кеширование — Redis Cluster.
        </p>
        <div className={styles.docCodeBlock}>
          {`# Запуск локального контура зависимостей
docker compose -f docker-compose.local.yml up -d`}
        </div>
        <h4 className={styles.docSectionTitle}>Первая неделя</h4>
        <p>
          Ваша главная цель на первую неделю — успешно развернуть локальное окружение, получить все необходимые
          доступы и собрать первый pull request в тестовом репозитории.
        </p>
      </div>
    ),
  },
  git: {
    title: 'Регламент Git и CI/CD',
    content: (
      <div className={styles.docContainer}>
        <p>
          Команда придерживается стандарта Git Flow с адаптацией под частые непрерывные релизы.
        </p>
        <h4 className={styles.docSectionTitle}>Ветвление и именование</h4>
        <p>
          Все новые задачи ответвляются от ветки <code>develop</code>. Ветка именуется по маске:
        </p>
        <div className={styles.docCodeBlock}>
          {`feature/TASK-123-short-description
bugfix/TASK-456-fix-null-pointer`}
        </div>
        <h4 className={styles.docSectionTitle}>Требования к Pull Request</h4>
        <p>
          1. Все тесты должны проходить локально и в пайплайне CI.<br />
          2. Покрытие нового кода тестами должно составлять не менее 80%.<br />
          3. Обязателен минимум один approval от наставника или старшего инженера.
        </p>
      </div>
    ),
  },
  access: {
    title: 'Сервис управления доступами',
    content: (
      <div className={styles.docContainer}>
        <p>
          Доступы к внутренним репозиториям, тестовым базам данных и мониторингу запрашиваются через корпоративный сервис доступа.
        </p>
        <h4 className={styles.docSectionTitle}>Автоматизация через AI-buddy</h4>
        <p>
          Вам не нужно заполнять сложные формы вручную: AI-buddy автоматически подготовит черновик заявки
          в чате. Вам останется только подтвердить отправку.
        </p>
        <h4 className={styles.docSectionTitle}>Регламентные сроки (SLA)</h4>
        <p>
          Заявки на тестовые репозитории согласуются наставником (Алексей Морозов). Среднее время согласования
          составляет до 2 часов в рабочее время команды.
        </p>
      </div>
    ),
  },
};

export const OnboardingPlaceholderPage: React.FC = () => {
  const [activeDocKey, setActiveDocKey] = useState<string | null>(null);

  const activeDoc = activeDocKey ? DOCUMENTATION[activeDocKey] : null;

  return (
    <div className={styles.container}>
      {/* Banner */}
      <section className={styles.heroBanner}>
        <div className={styles.heroInfo}>
          <h1 className={styles.heroTitle}>Мой план адаптации</h1>
          <p className={styles.heroSubtitle}>
            Индивидуальный план стажёра в команде Backend Core. Выполняйте шаги последовательно,
            изучайте регламенты и запрашивайте доступ к сервисам через ассистента.
          </p>
        </div>

        <div className={styles.heroProgressGroup}>
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
          <span className={styles.progressMeta}>Завершено 0 из 4 шагов. Шаг 1 открыт для выполнения.</span>
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
                Склонируйте репозиторий, запустите зависимости через Docker Compose и проверьте
                локальную сборку. Для загрузки кода запросите права на чтение через ассистента в чате.
              </p>

              <div className={styles.stepFooter}>
                <Link to="/chat?stepId=step_local_setup">
                  <Button variant="primary" size="sm">
                    Перейти в чат
                  </Button>
                </Link>
              </div>
            </article>

            {/* Step 2: Blocked */}
            <article className={`${styles.stepCard} ${styles.stepCardBlocked}`}>
              <div className={styles.stepCardHeader}>
                <div className={styles.stepTitleGroup}>
                  <span className={`${styles.stepNumber} ${styles.stepNumberBlocked}`}>2</span>
                  <h3 className={styles.stepTitle}>Знакомство с архитектурой и контрактами сервиса</h3>
                </div>
                <StatusBadge status="blocked" />
              </div>

              <p className={styles.stepDescription}>
                Изучите спецификацию OpenAPI, формат событий SSE и архитектуру внутренних микросервисов.
              </p>

              <div className={styles.stepFooter}>
                <span className={styles.blockedNotice}>Станет доступно после завершения шага 1</span>
              </div>
            </article>

            {/* Step 3: Blocked */}
            <article className={`${styles.stepCard} ${styles.stepCardBlocked}`}>
              <div className={styles.stepCardHeader}>
                <div className={styles.stepTitleGroup}>
                  <span className={`${styles.stepNumber} ${styles.stepNumberBlocked}`}>3</span>
                  <h3 className={styles.stepTitle}>Первый Pull Request в тестовый контур</h3>
                </div>
                <StatusBadge status="blocked" />
              </div>

              <p className={styles.stepDescription}>
                Внесите исправление в сервис задач, проверьте код линтером и отправьте Pull Request на ревью наставнику.
              </p>

              <div className={styles.stepFooter}>
                <span className={styles.blockedNotice}>Станет доступно после завершения шага 2</span>
              </div>
            </article>

            {/* Step 4: Blocked */}
            <article className={`${styles.stepCard} ${styles.stepCardBlocked}`}>
              <div className={styles.stepCardHeader}>
                <div className={styles.stepTitleGroup}>
                  <span className={`${styles.stepNumber} ${styles.stepNumberBlocked}`}>4</span>
                  <h3 className={styles.stepTitle}>Встреча 1-on-1 и итоги первой недели</h3>
                </div>
                <StatusBadge status="blocked" />
              </div>

              <p className={styles.stepDescription}>
                Обсудите с наставником результаты первой недели, ответьте на накопившиеся вопросы и согласуйте цели на первый спринт.
              </p>

              <div className={styles.stepFooter}>
                <span className={styles.blockedNotice}>Станет доступно после завершения шага 3</span>
              </div>
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
                Написать наставнику
              </Button>
            </Link>
          </div>

          {/* Useful links */}
          <div className={styles.widgetCard}>
            <h3 className={styles.widgetTitle}>База знаний и регламенты</h3>
            <nav className={styles.linksList}>
              <button
                type="button"
                className={styles.quickLink}
                onClick={() => setActiveDocKey('wiki')}
              >
                <span>Вики команды Backend</span>
              </button>
              <button
                type="button"
                className={styles.quickLink}
                onClick={() => setActiveDocKey('git')}
              >
                <span>Регламент Git и CI/CD</span>
              </button>
              <button
                type="button"
                className={styles.quickLink}
                onClick={() => setActiveDocKey('access')}
              >
                <span>Сервис доступов</span>
              </button>
            </nav>
          </div>
        </aside>
      </div>

      {/* Accessible Documentation Drawer */}
      <Drawer
        isOpen={Boolean(activeDoc)}
        onClose={() => setActiveDocKey(null)}
        title={activeDoc?.title}
        width="md"
      >
        {activeDoc?.content}
      </Drawer>
    </div>
  );
};
