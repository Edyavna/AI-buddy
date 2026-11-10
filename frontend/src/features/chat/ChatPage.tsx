import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, InlineError, Spinner } from '@/shared/ui';
import { ClientError, queryKeys } from '@/shared/api';
import { getConversation, getData, listConversations, listMessages } from './api';
import { planSchema, userSchema } from './schemas';
import { ChatController } from './controller';
import styles from './ChatPage.module.css';

export function ChatPage() {
  const { conversationId } = useParams();
  const [params] = useSearchParams();
  const stepId = params.get('stepId');
  const navigate = useNavigate(), client = useQueryClient();
  const [controller] = useState(() => new ChatController(client, () => navigate('/login', { replace: true })));
  const state = useSyncExternalStore(controller.subscribe, controller.snapshot);
  const [text, setText] = useState('');
  const [newMessages, setNewMessages] = useState(false);
  const activeUser = useRef<string | null>(null);
  const list = useRef<HTMLDivElement>(null), nearBottom = useRef(true);
  const busy = ['sending', 'streaming', 'recovering'].includes(state.phase);
  const me = useQuery({ queryKey: queryKeys.me(), queryFn: ({ signal }) => getData('/me', userSchema, signal) });
  const conversations = useQuery({ queryKey: queryKeys.conversations(), queryFn: ({ signal }) => listConversations(signal), enabled: !!me.data });
  const conversation = useQuery({ queryKey: queryKeys.conversation(conversationId ?? ''), queryFn: ({ signal }) => getConversation(conversationId!, signal), enabled: !!conversationId && !!me.data });
  const history = useQuery({ queryKey: queryKeys.messages(conversationId ?? ''), queryFn: ({ signal }) => listMessages(conversationId!, signal), enabled: !!conversation.data && !busy });
  const contextId = conversationId ? conversation.data?.step_id : stepId;
  const plan = useQuery({ queryKey: queryKeys.plan(), queryFn: ({ signal }) => getData('/onboarding/plan', planSchema, signal), enabled: !!contextId && !!me.data });
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
    // Internal replace after creation keeps the same controller and active request.
    if (controller.state.conversationId !== (conversationId ?? null)) {
      controller.reset(conversationId ?? null); setText(''); nearBottom.current = true;
    }
  }, [conversationId, controller]);
  useEffect(() => {
    if (!conversationId && me.data) {
      const restored = controller.restore(me.data.id);
      if (restored) setText(restored.text);
      else if (step) setText(previous => previous || `Помоги выполнить шаг: ${step.title}`);
    }
  }, [controller, conversationId, me.data, step]);
  useEffect(() => {
    if (history.data && !busy && controller.state.messages.length === 0) {
      controller.update({ messages: history.data });
      if (history.data.some(m => m.status === 'generating') && conversationId) void controller.recover(conversationId);
    }
  }, [history.data, busy, controller, conversationId]);
  const error = me.error ?? conversations.error ?? conversation.error ?? history.error ?? plan.error;
  useEffect(() => {
    if (error instanceof ClientError && error.status === 401) {
      controller.stop(); client.clear(); navigate('/login', { replace: true });
    }
  }, [error, controller, client, navigate]);
  useEffect(() => {
    if (nearBottom.current && list.current) list.current.scrollTop = list.current.scrollHeight;
    else if (state.messages.length) setNewMessages(true);
  }, [state.messages, state.tools]);
  const send = () => {
    if (!me.data || (conversationId && !conversation.data) || !contextReady || contextInvalid || !text.trim() || text.length > 8000 || busy) return;
    void controller.send(text, me.data.id, contextId ?? null, id => navigate(`/chat/${encodeURIComponent(id)}`, { replace: true }));
    setText('');
  };
  return <div className={styles.layout}>
    <aside className={styles.sidebar} aria-label="Диалоги">
      <Link to="/chat">Новый диалог</Link>
      {conversations.isLoading && <Spinner />}
      {conversations.data?.length === 0 && <p>Диалогов пока нет</p>}
      {conversations.data?.map(item => <Link key={item.id} aria-current={item.id === conversationId ? 'page' : undefined} to={`/chat/${encodeURIComponent(item.id)}`}>{item.title}</Link>)}
    </aside>
    <section className={styles.chat} aria-label="Чат с помощником">
      <header><h1>Помощник стажёра</h1><p>{step ? `Шаг: ${step.title}` : 'Инструкции, доступы и первые задачи'}</p></header>
      {error && <InlineError message={error.message} onRetry={() => { void client.invalidateQueries(); }} />}
      {contextInvalid && <InlineError message="Шаг не найден в вашем плане. Вернитесь к плану адаптации." />}
      {(me.isPending || (conversationId && history.isLoading)) && <Spinner />}
      <div ref={list} className={styles.messages} onScroll={() => {
        const el = list.current!; nearBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
        if (nearBottom.current) setNewMessages(false);
      }}>
        {!state.messages.length && !busy && <div className={styles.welcome}><h2>С чего начнём?</h2><p>Задайте вопрос о настройке окружения или выберите шаг в плане.</p><Link to="/onboarding">Открыть мой план</Link></div>}
        {state.messages.map(message => <article key={message.id} className={message.role === 'user' ? styles.user : styles.assistant}>
          <strong>{message.role === 'user' ? 'Вы' : 'AI-buddy'}</strong>
          <p>{message.text || (message.status === 'generating' ? 'Готовлю ответ…' : 'Ответ без текста')}</p>
          {['failed', 'interrupted'].includes(message.status) && <InlineError message={message.error?.error.message ?? 'Ответ прерван. Можно задать вопрос заново.'} />}
        </article>)}
      </div>
      {newMessages && <Button variant="secondary" onClick={() => { nearBottom.current = true; setNewMessages(false); if (list.current) list.current.scrollTop = list.current.scrollHeight; }}>Новые сообщения</Button>}
      <div role="status" aria-live="polite">{state.phase === 'sending' ? 'Отправляем вопрос…' : state.phase === 'recovering' ? 'Восстанавливаем ответ…' : ''}{Object.values(state.tools).map((label, i) => <p key={i}>{label}</p>)}</div>
      {state.error && <InlineError message={state.error} />}
      {!busy && controller.canRetry && me.data && <Button variant="secondary" onClick={() => void controller.retry(me.data!.id, id => navigate(`/chat/${encodeURIComponent(id)}`, { replace: true }))}>Повторить исходный вопрос</Button>}
      {state.conversationId && <Button variant="ghost" disabled={busy} onClick={() => void controller.recover(state.conversationId!)}>Обновить историю</Button>}
      <form className={styles.composer} onSubmit={event => { event.preventDefault(); send(); }}>
        <label htmlFor="chat-question">Ваш вопрос</label>
        <textarea id="chat-question" value={text} maxLength={8000} onChange={event => setText(event.target.value)} onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && event.keyCode !== 229) { event.preventDefault(); send(); }
        }} placeholder="Спросите о чём-нибудь" />
        <div><small>{text.length}/8000 · Shift+Enter — новая строка</small><Button type="submit" disabled={busy || !text.trim() || !contextReady || contextInvalid || !me.data || !!conversation.error}>Отправить</Button></div>
      </form>
    </section>
  </div>;
}
