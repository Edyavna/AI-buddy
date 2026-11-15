import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { Button, InlineError, Spinner } from '@/shared/ui';
import { ClientError, queryKeys } from '@/shared/api';
import { abortSessionRequests } from '@/shared/api/session-signal';
import { queryRequestSignal } from '@/shared/api/query-access';
import { getConversation, getData, listConversations, listMessages, setConversationArchived } from './api';
import { userSchema } from './schemas';
import { usePlan } from '@/features/onboarding';
import { ChatController } from './controller';
import { QuestionComposer } from '@/features/hr';
import { canEscalateMessage, MentorQuestionPanel } from '@/features/mentor-questions';
import { MeetingSuggestion } from '@/features/onboarding-operations';
import { AccessRequestPanel } from '@/features/access-requests';
import { DocumentDrawer, MarkdownContent } from '@/features/documents';
import { MemorySuggestions } from '@/features/personal-memory';
import styles from './ChatPage.module.css';
import { chatAccessDenied, chatSignal, clearChatAccess } from './access';

export function ChatPage({ mentor = false }: { mentor?: boolean } = {}) {
  const chatPath = mentor ? "/mentor/chat" : "/chat";
  const { conversationId } = useParams();
  const [params] = useSearchParams();
  const newMarkdown = mentor && !conversationId && params.get('mode') === 'markdown';
  const previousNewMarkdown = useRef(newMarkdown);
  const stepId = mentor ? null : params.get('stepId');
  const requestedDirection = mentor ? null : params.get('direction');
  const preferredDirection = requestedDirection === 'mentor' || requestedDirection === 'hr' ? requestedDirection : undefined;
  const {hash}=useLocation();
  const navigate = useNavigate(), client = useQueryClient();
  const [controller] = useState(() => new ChatController(client, () => navigate('/login', { replace: true })));
  const state = useSyncExternalStore(controller.subscribe, controller.snapshot);
  const [text, setText] = useState('');
  const [newMessages, setNewMessages] = useState(false);
  const [documentId, setDocumentId] = useState<string | null>(null);
  const [sourceMessageId,setSourceMessageId]=useState<string>();
  const closeDocument = useCallback(() => {setDocumentId(null);setSourceMessageId(undefined);void client.invalidateQueries({queryKey:['question-context']});}, [client]);
  function openSource(messageId:string,id:string){setSourceMessageId(messageId);setDocumentId(id);}
  const composer = useRef<HTMLTextAreaElement>(null);
  const focusComposer = useCallback(() => composer.current?.focus(), []);
  const activeUser = useRef<string | null>(null);
  const list = useRef<HTMLDivElement>(null), nearBottom = useRef(true);
  const messageContent = useRef<HTMLDivElement>(null);
  const visitedSource=useRef('');
  useEffect(()=>{
    const key=(state.conversationId??'')+hash;
    if(!hash.startsWith('#message-')||visitedSource.current===key)return;
    let id:string;try{id=decodeURIComponent(hash.slice(1));}catch{return;}
    const target=document.getElementById(id);
    if(target){nearBottom.current=false;target.scrollIntoView?.({block:'center'});visitedSource.current=key;}
  },[hash,state.conversationId,state.messages.length]);
  const busy = ['sending', 'streaming', 'recovering'].includes(state.phase);
  const memorySource=state.messages.filter(message=>message.role==='user'&&state.messages.some(answer=>answer.role==='assistant'&&answer.status==='completed'&&answer.in_reply_to_message_id===message.id)).at(-1)?.id;
  function denyChat(key?: QueryKey) {
    clearChatAccess(client, key); controller.denyAccess(key ? '' : undefined);
    setText(''); setDocumentId(null); setSourceMessageId(undefined);
  }
  async function readChat<T>(key: QueryKey, load: (signal: AbortSignal) => Promise<T>, owner: AbortSignal) {
    const signal = chatSignal(client, owner);
    try {
      const data = await load(signal);
      signal.throwIfAborted();
      return data;
    } catch (error) {
      if (!signal.aborted && chatAccessDenied(error)) denyChat(key);
      throw error;
    }
  }
  const me = useQuery({ queryKey: queryKeys.me(), queryFn: ({ signal }) => getData('/me', userSchema, signal) });
  const [showArchive, setShowArchive] = useState(false);
  const conversationsKey = showArchive ? [...queryKeys.conversations(), 'archive'] : queryKeys.conversations();
  const conversations = useQuery({ queryKey: conversationsKey, queryFn: ({ signal }) => readChat(conversationsKey, request => listConversations(request, showArchive), signal), enabled: !!me.data });
  const archive = useMutation({
    mutationFn: async ({ id, archived }: { id: string; archived: boolean }) => {
      const key = queryKeys.conversation(id);
      const signal = chatSignal(client, queryRequestSignal(client, key));
      try {
        const result = await setConversationArchived(id, archived, signal);
        signal.throwIfAborted();
        if (result.data) client.setQueryData(key, result.data);
        await client.invalidateQueries({ queryKey: queryKeys.conversations() });
      } catch (error) {
        if (signal.aborted) return;
        if (chatAccessDenied(error)) denyChat();
        throw error;
      }
    },
  });
  const conversationKey = queryKeys.conversation(conversationId ?? '');
  const messagesKey = queryKeys.messages(conversationId ?? '');
  const conversation = useQuery({ queryKey: conversationKey, queryFn: ({ signal }) => readChat(conversationKey, request => getConversation(conversationId!, request), signal), enabled: !!conversationId && !!me.data });
  const history = useQuery({ queryKey: messagesKey, queryFn: ({ signal }) => readChat(messagesKey, request => listMessages(conversationId!, request), signal), enabled: !!conversation.data && !busy });
  const markdown = newMarkdown || conversation.data?.markdown === true;
  const contextId = mentor ? null : conversationId ? conversation.data?.step_id : stepId;
  const plan = usePlan(Boolean(contextId && me.data));
  const step = plan.data?.steps.find(item => item.id === contextId);
  const contextInvalid = !!contextId && !!plan.data && !step;
  const contextReady = !contextId || !!step;
  useEffect(() => {
    if (!me.data) return;
    if (activeUser.current && activeUser.current !== me.data.id) {
      controller.reset(conversationId ?? null); setText('');
    }
    activeUser.current = me.data.id;
  }, [me.data, conversationId, controller]);

  useEffect(() => { controller.activate(); return () => controller.stop(); }, [controller]);
  useEffect(() => {
    // A new chat has no history read to reactivate it after access is restored.
    // Only a successful list read may start a fresh controller generation.
    if (!conversationId && conversations.isSuccess && conversations.data) controller.activate();
  }, [conversationId, conversations.isSuccess, conversations.data, conversations.dataUpdatedAt, controller]);
  useEffect(() => {
    // Internal replace after creation keeps the same controller and active request.
    if (controller.state.conversationId !== (conversationId ?? null) || (!conversationId && previousNewMarkdown.current !== newMarkdown)) {
      controller.reset(conversationId ?? null); setText(''); setDocumentId(null); nearBottom.current = true;
    }
    previousNewMarkdown.current = newMarkdown;
  }, [conversationId, controller, newMarkdown]);
  useEffect(() => {
    if (!conversationId && me.data) {
      const restored = controller.restore(me.data.id, newMarkdown);
      if (restored) setText(restored.text);
      else if (step) setText(previous => previous || `Помоги выполнить шаг: ${step.title}`);
      else if (mentor) setText('');
    }
  }, [controller, conversationId, me.data, step, newMarkdown, mentor]);
  useEffect(() => {
    if (history.data && !busy && controller.state.messages.length === 0) {
      controller.activate();
      controller.update({ messages: history.data, phase: 'idle', error: '' });
      if (history.data.some(m => m.status === 'generating') && conversationId) void controller.recover(conversationId);
    }
  }, [history.data, busy, controller, conversationId]);
  const error = me.error ?? conversations.error ?? conversation.error ?? history.error ?? (mentor ? null : plan.error);
  useEffect(() => {
    if (error instanceof ClientError && error.status === 401) {
      controller.stop(); abortSessionRequests(client); client.clear(); navigate('/login', { replace: true });
    }
  }, [error, controller, client, navigate]);
  useEffect(() => {
    if (nearBottom.current && list.current) list.current.scrollTop = list.current.scrollHeight;
    else if (state.messages.length) setNewMessages(true);
  }, [state.messages, state.tools]);
  useEffect(() => {
    if (!messageContent.current || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(() => {
      if (nearBottom.current && list.current) list.current.scrollTop = list.current.scrollHeight;
      else setNewMessages(true);
    });
    observer.observe(messageContent.current);
    return () => observer.disconnect();
  }, []);
  const openConversation = (id: string) => navigate(`${chatPath}/${encodeURIComponent(id)}${preferredDirection ? `?direction=${preferredDirection}` : ''}`, { replace: true });
  const send = () => {
    if (!me.data || (conversationId && !conversation.data) || !contextReady || contextInvalid || !text.trim() || text.length > 8000 || busy) return;
    void controller.send(text, me.data.id, contextId ?? null, openConversation, { markdown });
    setText('');
  };
  return <div className={styles.layout} data-chat-layout>
    <aside className={styles.sidebar} aria-label="Диалоги">
      <div className={styles.sidebarActions}>
      <Link to={chatPath} className={styles.newChat}><span aria-hidden="true">+</span> Новый диалог</Link>
      <Button variant="secondary" aria-pressed={showArchive} onClick={() => setShowArchive(value => !value)}>{showArchive ? 'Активные диалоги' : 'Архив'}</Button>
      </div>
      <div className={styles.conversationList}>
      {conversations.isLoading && <Spinner />}
      {conversations.data?.length === 0 && <p>{showArchive ? 'Архив пуст' : 'Диалогов пока нет'}</p>}
      {conversations.data?.map(item => {
        const action = showArchive ? 'Вернуть из архива' : 'Архивировать диалог';
        return <div key={item.id} className={styles.conversationRow}>
          <Link aria-current={item.id === conversationId ? 'page' : undefined} to={`${chatPath}/${encodeURIComponent(item.id)}`}>{item.title}</Link>
          <button type="button" className={styles.archiveAction} aria-label={`${action}: ${item.title}`} title={action}
            disabled={archive.isPending || (busy && item.id === conversationId)}
            onClick={() => archive.mutate({ id: item.id, archived: !showArchive })}>
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M4 7h16V3H4z M6 7v13h12V7" />
              {showArchive ? <path d="M12 16V10m-3 3 3-3 3 3" /> : <path d="M12 10v6m-3-3 3 3 3-3" />}
            </svg>
          </button>
        </div>;
      })}
      </div>
    </aside>
    <section className={styles.chat} aria-label="Чат с помощником">
      <header><h1>{markdown ? 'Подготовка Markdown-инструкции' : mentor ? 'Помощник наставника' : 'Помощник стажёра'}</h1><p>{mentor ? 'Вопросы к ИИ' : step ? `Шаг: ${step.title}` : 'Инструкции, доступы и первые задачи'}</p>{!mentor && me.data?.role === 'intern' && <Link to="/memory">Моя память</Link>}</header>
      {archive.error && <InlineError message={archive.error.message} />}
      {error && <InlineError message={error.message} onRetry={() => { void client.invalidateQueries(); }} />}
      {contextInvalid && <InlineError message="Шаг не найден в вашем плане. Вернитесь к плану адаптации." />}
      {(me.isPending || (conversationId && history.isLoading)) && <Spinner />}
      <div ref={list} className={styles.messages} role="log" aria-label="История сообщений" aria-live="off" onScroll={() => {
        const el = list.current!; nearBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
        if (nearBottom.current) setNewMessages(false);
      }}>
        <div ref={messageContent} className={styles.messageContent}>
        {!state.messages.length && !busy && <div className={styles.welcome}><h2>С чего начнём?</h2><p>{markdown ? 'Опишите, какую инструкцию нужно подготовить.' : mentor ? 'Задайте вопрос помощнику.' : 'Задайте вопрос о настройке окружения или выберите шаг в плане.'}</p>{!mentor && <Link to="/onboarding">Открыть мой план</Link>}</div>}
        {state.messages.map(message => {
          const canEscalate = !mentor && canEscalateMessage(message);
          const original = canEscalate ? state.messages.find(m => m.id === message.in_reply_to_message_id && m.role === 'user') : undefined;
          return <article key={message.id} id={`message-${message.id}`} className={message.role === 'user' ? styles.user : styles.assistant}>
            <strong>{message.role === 'user' ? 'Вы' : 'AI-buddy'}</strong>
            {message.role === 'assistant'
              ? <MarkdownContent content={message.text || (message.status === 'generating' ? 'Готовлю ответ…' : 'Ответ без текста')} />
              : <p>{message.text}</p>}
            {message.sources.length > 0 && <ul className={styles.sources} aria-label="Источники ответа">
              {message.sources.map(source => <li key={source.id}>
                <button type="button" className={styles.source} onClick={() => openSource(message.id,source.document_id)}>
                  <strong>{source.title}</strong><span>{source.excerpt}</span>
                  {source.version && source.direction === 'hr' && <span>Версия {source.version}{source.updated_at && ` · ${new Date(source.updated_at).toLocaleDateString('ru-RU')}`}</span>}
                </button>
              </li>)}
            </ul>}
            {!mentor && !conversation.data?.archived && me.data?.role === 'intern' && !busy &&
              message.role === 'assistant' && message.status !== 'generating' && state.messages.at(-1)?.id === message.id &&
              <MeetingSuggestion messageID={message.id} owner={me.data.id} />}
            {markdown && message.role === 'assistant' && message.status === 'completed' && message.outcome === 'answered' &&
              <a download="instruction.md" href={`data:text/markdown;charset=utf-8,${encodeURIComponent(message.text)}`}>Скачать .md</a>}
            {!mentor && [...new Set(message.request_ids)].map(requestId => <div className={styles.request} key={requestId}>
              <AccessRequestPanel requestId={requestId} onDiscuss={focusComposer} />
            </div>)}
            {!mentor && message.role === 'assistant' && message.in_reply_to_message_id && message.status !== 'generating' &&
              <QuestionComposer messageId={message.id} preferredDirection={preferredDirection} onOpenDocument={id=>{setSourceMessageId(undefined);setDocumentId(id);}} onOpenContextDocument={id=>openSource(message.id,id)}/>}
            {canEscalate && !message.routing && me.data &&
              <MentorQuestionPanel key={me.data.id} messageId={message.id} onOpenDocument={id=>{setSourceMessageId(undefined);setDocumentId(id);}} existingOnly
                canEscalate={canEscalate} isChatBusy={busy} canRepeatQuestion={!!original && contextReady && !contextInvalid && !error}
                onRepeatQuestion={() => {
                  if (!original || busy || !contextReady || contextInvalid || error) return;
                  void controller.send(original.text, me.data.id, contextId ?? null,
                    openConversation, { newMessage: true });
                }} />}
            {['failed', 'interrupted'].includes(message.status) && <InlineError message={message.error?.error.message ?? 'Ответ прерван. Можно задать вопрос заново.'} />}
            {mentor && !busy && me.data && ['failed', 'interrupted'].includes(message.status) && state.messages.at(-1)?.id === message.id && (
              <Button variant="secondary" onClick={() => {
                const question = state.messages.find(item => item.id === message.in_reply_to_message_id);
                if (question) void controller.send(question.text, me.data!.id, null, openConversation, { newMessage: true });
              }}>Повторить запрос</Button>
            )}
          </article>;
        })}
        {!mentor&&memorySource&&me.data?.role==='intern'&&!busy&&!error&&<MemorySuggestions key={`${me.data.id}:${memorySource}`} messageId={memorySource}/>}
        </div>
      </div>
      {newMessages && <Button variant="secondary" onClick={() => { nearBottom.current = true; setNewMessages(false); if (list.current) list.current.scrollTop = list.current.scrollHeight; }}>Новые сообщения</Button>}
      <div role="status" aria-live="polite">{state.phase === 'sending' ? 'Отправляем вопрос…' : state.phase === 'recovering' ? 'Восстанавливаем ответ…' : ''}{Object.values(state.tools).map((label, i) => <p key={i}>{label}</p>)}</div>
      {!error && !archive.error && state.error && <InlineError message={state.error} />}
      {!busy && controller.canRetry && me.data && <Button variant="secondary" onClick={() => void controller.retry(me.data!.id, openConversation)}>Повторить исходный вопрос</Button>}
      {state.conversationId && <Button variant="ghost" disabled={busy} onClick={() => void controller.recover(state.conversationId!)}>Обновить историю</Button>}
      <form className={styles.composer} onSubmit={event => { event.preventDefault(); send(); }}>
        <label htmlFor="chat-question">Ваш вопрос</label>
        <textarea ref={composer} id="chat-question" value={text} maxLength={8000} onChange={event => setText(event.target.value)} onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && event.keyCode !== 229) { event.preventDefault(); send(); }
        }} placeholder="Спросите о чём-нибудь" />
        <div><small>{text.length}/8000 · Shift+Enter — новая строка</small><Button type="submit" disabled={busy || !text.trim() || !contextReady || contextInvalid || !me.data || !!conversation.error}>Отправить</Button></div>
      </form>
    </section>
    <DocumentDrawer documentId={documentId} onClose={closeDocument} sourceMessageId={sourceMessageId}/>
  </div>;
}
