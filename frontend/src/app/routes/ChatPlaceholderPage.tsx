import React, { useState } from 'react';
import { useParams, useSearchParams, Link } from 'react-router-dom';
import { Button, StatusBadge, Drawer, Dialog } from '@/shared/ui';
import styles from './ChatPlaceholderPage.module.css';

interface ChatMessage {
  id: string;
  sender: 'user' | 'bot';
  text: string;
  time: string;
  hasRequestCard?: boolean;
  sources?: { id: string; label: string }[];
}

interface DocArticle {
  title: string;
  content: React.ReactNode;
}

const DOCUMENTATION: Record<string, DocArticle> = {
  local_setup: {
    title: 'local_setup.md (стр. 4) — Доступы к репозиториям',
    content: (
      <div className={styles.docContainer}>
        <p>
          Все репозитории команды Backend Core размещаются в защищенном контуре GitLab и GitHub Enterprise.
          По умолчанию новым сотрудникам назначается гостевой профиль без прав на клонирование исходного кода.
        </p>
        <h4 className={styles.docSectionTitle}>Порядок получения доступа</h4>
        <p>
          1. Сформировать заявку через AI-buddy или напрямую в сервисе Access Manager.<br />
          2. Указать необходимый уровень прав: <code>read</code> (чтение для ознакомления и локальной сборки) или <code>write</code> (создание веток и MR).<br />
          3. Согласование заявки руководителем команды (нормативный срок SLA: до 2 рабочих часов).<br />
          4. Автоматическая синхронизация SSH-ключей в течение 5 минут после одобрения.
        </p>
        <h4 className={styles.docSectionTitle}>Команда для клонирования после подтверждения</h4>
        <div className={styles.docCodeBlock}>
          git clone git@github.com:avito-tech/repo_demo_backend.git
        </div>
      </div>
    ),
  },
};

export const ChatPlaceholderPage: React.FC = () => {
  const { conversationId } = useParams<{ conversationId?: string }>();
  const [searchParams] = useSearchParams();
  const stepId = searchParams.get('stepId');

  const [inputValue, setInputValue] = useState('');
  const [isConfirmed, setIsConfirmed] = useState(false);
  const [isConfirmDialogOpen, setIsConfirmDialogOpen] = useState(false);
  const [activeDocKey, setActiveDocKey] = useState<string | null>(null);
  const [isMobileSidebarOpen, setIsMobileSidebarOpen] = useState(false);

  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: 'msg-1',
      sender: 'user',
      text: 'Не удается склонировать репозиторий проекта, возвращается ошибка 403 Forbidden. Как получить доступ?',
      time: '12:30',
    },
    {
      id: 'msg-2',
      sender: 'bot',
      text: 'Для работы требуется доступ на чтение к репозиторию repo_demo_backend. Подготовлен черновик заявки:',
      time: '12:31',
      hasRequestCard: true,
      sources: [{ id: 'local_setup', label: 'local_setup.md, стр. 4' }],
    },
  ]);

  const handleSendMessage = () => {
    const trimmed = inputValue.trim();
    if (!trimmed) return;

    const now = new Date();
    const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;

    const userMessage: ChatMessage = {
      id: `msg-${Date.now()}`,
      sender: 'user',
      text: trimmed,
      time: timeStr,
    };

    setMessages((prev) => [...prev, userMessage]);
    setInputValue('');

    // Simulate AI response
    setTimeout(() => {
      const botResponse: ChatMessage = {
        id: `msg-${Date.now() + 1}`,
        sender: 'bot',
        text: 'Принято! Обрабатываю ваш вопрос по шагу адаптации. Если вам требуется доступ к дополнительным сервисам или репозиториям, вы можете запросить их через заявку выше.',
        time: timeStr,
      };
      setMessages((prev) => [...prev, botResponse]);
    }, 500);
  };

  const handleConfirmRequest = () => {
    setIsConfirmed(true);
    setIsConfirmDialogOpen(false);
  };

  const activeDoc = activeDocKey ? DOCUMENTATION[activeDocKey] : null;

  return (
    <div className={styles.container}>
      {/* Header */}
      <div className={styles.header}>
        <div className={styles.titleRow}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
            <button
              type="button"
              className={styles.mobileSidebarToggle}
              onClick={() => setIsMobileSidebarOpen(!isMobileSidebarOpen)}
              aria-label="Открыть список диалогов"
            >
              ☰ Диалоги
            </button>
            <h1 className={styles.title}>Чат с наставником</h1>
          </div>

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
        {/* Mobile Backdrop */}
        {isMobileSidebarOpen && (
          <div
            className={styles.mobileBackdrop}
            onClick={() => setIsMobileSidebarOpen(false)}
            aria-hidden="true"
          />
        )}

        {/* Sidebar: Dialogs */}
        <aside
          className={`${styles.conversationsSidebar} ${isMobileSidebarOpen ? styles.conversationsSidebarOpen : ''}`}
        >
          <div className={styles.sidebarHeader}>
            <span className={styles.sidebarTitle}>Диалоги</span>
            <Link to="/chat" onClick={() => setIsMobileSidebarOpen(false)}>
              <Button size="sm" variant="ghost">
                + Новый
              </Button>
            </Link>
          </div>

          <div className={styles.conversationsList}>
            <Link
              to="/chat?stepId=step_local_setup"
              onClick={() => setIsMobileSidebarOpen(false)}
              className={`${styles.conversationItem} ${!conversationId ? styles.conversationItemActive : ''}`}
            >
              <span className={styles.convItemTitle}>Настройка окружения</span>
              <span className={styles.convItemMeta}>Первый шаг адаптации</span>
            </Link>

            <Link
              to="/chat/conv-123"
              onClick={() => setIsMobileSidebarOpen(false)}
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
            {messages.map((msg) => {
              const isUser = msg.sender === 'user';
              return (
                <div
                  key={msg.id}
                  className={`${styles.messageRow} ${isUser ? styles.userMessageRow : styles.botMessageRow}`}
                >
                  <div className={`${styles.messageBubble} ${isUser ? styles.userBubble : styles.botBubble}`}>
                    <p>{msg.text}</p>

                    {/* Embedded Access Request Card */}
                    {msg.hasRequestCard && (
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
                            onClick={() => setIsConfirmDialogOpen(true)}
                          >
                            Подтвердить отправку
                          </Button>
                        ) : (
                          <div style={{ color: 'var(--avito-green-text)', fontSize: '13px', fontWeight: 600 }}>
                            Заявка отправлена. Ожидает решения в сервисе доступа.
                          </div>
                        )}
                      </div>
                    )}

                    {/* Source References */}
                    {msg.sources && msg.sources.length > 0 && (
                      <div className={styles.sourcesGroup}>
                        <span>Источники:</span>
                        {msg.sources.map((src) => (
                          <button
                            key={src.id}
                            type="button"
                            className={styles.sourceChip}
                            onClick={() => setActiveDocKey(src.id)}
                          >
                            {src.label}
                          </button>
                        ))}
                      </div>
                    )}

                    <span className={styles.messageTime}>{msg.time}</span>
                  </div>
                </div>
              );
            })}
          </div>

          {/* Composer */}
          <form
            className={styles.composerBar}
            onSubmit={(e) => {
              e.preventDefault();
              handleSendMessage();
            }}
          >
            <textarea
              className={styles.composerInput}
              placeholder="Задайте вопрос по текущей задаче... (Enter — отправить, Shift+Enter — новая строка)"
              rows={1}
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault();
                  handleSendMessage();
                }
              }}
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
                aria-hidden="true"
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

      {/* Confirmation Dialog for Access Request */}
      <Dialog
        isOpen={isConfirmDialogOpen}
        onClose={() => setIsConfirmDialogOpen(false)}
        title="Подтверждение заявки на доступ"
        footer={
          <>
            <Button
              variant="secondary"
              onClick={() => setIsConfirmDialogOpen(false)}
            >
              Отмена
            </Button>
            <Button
              variant="primary"
              onClick={handleConfirmRequest}
            >
              Отправить заявку
            </Button>
          </>
        }
      >
        <div>
          <p style={{ margin: 0, fontSize: 'var(--font-size-sm)', color: 'var(--color-text-secondary)' }}>
            Проверьте параметры доступа перед формированием тикета в Access Manager:
          </p>

          <div className={styles.dialogMetaGrid}>
            <span className={styles.dialogMetaLabel}>Ресурс:</span>
            <span className={styles.dialogMetaValue}>repo_demo_backend</span>

            <span className={styles.dialogMetaLabel}>Запрашиваемые права:</span>
            <span className={styles.dialogMetaValue}>Чтение (read)</span>

            <span className={styles.dialogMetaLabel}>Согласующий:</span>
            <span className={styles.dialogMetaValue}>Алексей Морозов (Team Lead Backend)</span>

            <span className={styles.dialogMetaLabel}>Нормативный срок:</span>
            <span className={styles.dialogMetaValue}>2 часа (SLA команды)</span>
          </div>

          <div className={styles.dialogCallout}>
            После подтверждения заявка поступит в корпоративную систему доступов. Уведомление об одобрении придет в мессенджер.
          </div>
        </div>
      </Dialog>

      {/* Documentation Drawer */}
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
