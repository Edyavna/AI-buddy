import React, { useState } from 'react';
import { useParams, useSearchParams, Link } from 'react-router-dom';
import { Button, StatusBadge } from '@/shared/ui';
import styles from './ChatPlaceholderPage.module.css';

export const ChatPlaceholderPage: React.FC = () => {
  const { conversationId } = useParams<{ conversationId?: string }>();
  const [searchParams] = useSearchParams();
  const stepId = searchParams.get('stepId');
  const [inputValue, setInputValue] = useState('');
  const [isConfirmed, setIsConfirmed] = useState(false);

  return (
    <div className={styles.container}>
      {/* Header */}
      <div className={styles.header}>
        <div className={styles.titleRow}>
          <h1 className={styles.title}>Чат с наставником</h1>
          {conversationId ? (
            <StatusBadge status="in_progress" label={`Диалог: ${conversationId}`} />
          ) : (
            <StatusBadge status="draft" label="Новый диалог" />
          )}
        </div>

        {stepId && (
          <div className={styles.stepBanner}>
            <span className={styles.stepLabel}>Контекст шага адаптации:</span>
            <code className={styles.stepCode}>{stepId}</code>
            <Link to="/onboarding" className={styles.backLink}>
              ← Вернуться к плану
            </Link>
          </div>
        )}
      </div>

      {/* Main Chat Interface */}
      <div className={styles.chatWindow}>
        {/* Sidebar: Dialogs */}
        <aside className={styles.conversationsSidebar}>
          <div className={styles.sidebarHeader}>
            <span className={styles.sidebarTitle}>Диалоги</span>
            <Link to="/chat">
              <Button size="sm" variant="ghost">
                + Новый
              </Button>
            </Link>
          </div>

          <div className={styles.conversationsList}>
            <Link
              to="/chat?stepId=step_local_setup"
              className={`${styles.conversationItem} ${!conversationId ? styles.conversationItemActive : ''}`}
            >
              <span className={styles.convItemTitle}>Настройка окружения</span>
              <span className={styles.convItemMeta}>Первый шаг адаптации</span>
            </Link>

            <Link
              to="/chat/conv-123"
              className={`${styles.conversationItem} ${conversationId === 'conv-123' ? styles.conversationItemActive : ''}`}
            >
              <span className={styles.convItemTitle}>Вопрос по Git и веткам</span>
              <span className={styles.convItemMeta}>Вчера, 3 сообщения</span>
            </Link>
          </div>
        </aside>

        {/* Chat Feed & Composer */}
        <div className={styles.chatMainArea}>
          {/* Top Info inside active chat */}
          <div className={styles.chatTopInfo}>
            <div className={styles.botStatusGroup}>
              <div className={styles.botAvatar}>AI</div>
              <div>
                <div className={styles.botTitle}>Ассистент стажера</div>
                <div className={styles.botStatus}>
                  <span className={styles.onlineDot} />
                  <span>В сети</span>
                </div>
              </div>
            </div>

            <StatusBadge status="in_progress" label="Активный диалог" size="sm" />
          </div>

          {/* Messages Feed */}
          <div className={styles.messagesFeed}>
            {/* User Message */}
            <div className={`${styles.messageRow} ${styles.userMessageRow}`}>
              <div className={`${styles.messageBubble} ${styles.userBubble}`}>
                <p>
                  Не удается склонировать репозиторий проекта, возвращается ошибка <code>403 Forbidden</code>. Как получить доступ?
                </p>
                <span className={styles.messageTime}>12:30</span>
              </div>
            </div>

            {/* AI-Buddy Message */}
            <div className={`${styles.messageRow} ${styles.botMessageRow}`}>
              <div className={`${styles.messageBubble} ${styles.botBubble}`}>
                <p>
                  Для работы требуется доступ на чтение к репозиторию <strong>repo_demo_backend</strong>. Подготовлен черновик заявки:
                </p>

                {/* Embedded Access Request Card */}
                <div className={styles.requestPreviewCard}>
                  <div className={styles.requestCardHeader}>
                    <span className={styles.requestCardTitle}>
                      Заявка на доступ к ресурсу
                    </span>
                    <StatusBadge
                      status={isConfirmed ? 'pending' : 'draft'}
                      label={isConfirmed ? 'На согласовании (ACCESS-1001)' : 'Черновик'}
                      size="sm"
                    />
                  </div>

                  <div className={styles.requestDetailRow}>
                    <span>Ресурс: <strong>repo_demo_backend</strong></span>
                    <span>Права: <strong>Чтение (read)</strong></span>
                  </div>

                  {!isConfirmed ? (
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={() => setIsConfirmed(true)}
                    >
                      Подтвердить отправку
                    </Button>
                  ) : (
                    <div style={{ color: 'var(--avito-green-text)', fontSize: '13px', fontWeight: 600 }}>
                      Заявка отправлена. Ожидает решения в сервисе доступа.
                    </div>
                  )}
                </div>

                <div className={styles.sourcesGroup}>
                  <span>Источники:</span>
                  <span
                    className={styles.sourceChip}
                    onClick={() => alert('Открытие регламента local_setup.md')}
                  >
                    local_setup.md, стр. 4
                  </span>
                </div>

                <span className={styles.messageTime}>12:31</span>
              </div>
            </div>
          </div>

          {/* Composer */}
          <form
            className={styles.composerBar}
            onSubmit={(e) => {
              e.preventDefault();
              if (inputValue.trim()) {
                alert(`Сообщение отправлено: ${inputValue}`);
                setInputValue('');
              }
            }}
          >
            <input
              type="text"
              className={styles.composerInput}
              placeholder="Задайте вопрос по текущей задаче..."
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
            />
            <button
              type="submit"
              className={styles.composerSendButton}
              title="Отправить сообщение"
              aria-label="Отправить сообщение"
            >
              <svg
                className={styles.composerIcon}
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <line x1="22" y1="2" x2="11" y2="13" />
                <polygon points="22 2 15 22 11 13 2 9 22 2" />
              </svg>
            </button>
          </form>

          {/* Integration notification for FE1 */}
          <div className={styles.integrationNotice}>
            <span>Слот диалога готов к интеграции</span>
            <span>FE1 подключает SSE-поток и историю диалогов</span>
          </div>
        </div>
      </div>
    </div>
  );
};
