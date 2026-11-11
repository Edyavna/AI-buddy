import type { QueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { ClientError, openApiStream, queryKeys } from '@/shared/api';
import { acceptAccessRequest } from '@/features/access-requests';
import { createConversation, listMessages } from './api';
import { applyEvent, protocolError, readSse, type StreamState } from './stream';

const pendingSchema = z.object({ conversationId: z.uuid(), messageId: z.uuid(), text: z.string().min(1).max(8000), stepId: z.string().nullable() });
type Pending = z.infer<typeof pendingSchema>;
export type State = StreamState & { phase: 'idle' | 'sending' | 'streaming' | 'recovering' | 'failed'; error: string; conversationId: string | null };
export class ChatController {
  state: State = { messages: [], tools: {}, assistantId: null, userId: null, phase: 'idle', error: '', conversationId: null };
  private listeners = new Set<() => void>();
  private abort = new AbortController();
  private pending: Pending | null = null;
  constructor(private client: QueryClient, private unauthorized: () => void) {}
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  snapshot = () => this.state;
  update(patch: Partial<State>) { this.state = { ...this.state, ...patch }; this.listeners.forEach(listener => listener()); }
  stop() { this.abort.abort(); }
  activate() { if (this.abort.signal.aborted) this.abort = new AbortController(); }
  retry(userId: string, navigate: (id: string) => void) {
    if (this.pending) return this.send(this.pending.text, userId, this.pending.stepId, navigate);
  }
  get canRetry() { return this.pending !== null; }
  reset(id: string | null) {
    this.stop(); this.abort = new AbortController(); this.pending = null;
    this.update({ conversationId: id, messages: [], tools: {}, assistantId: null, userId: null, phase: 'idle', error: '' });
  }
  private fail(error: unknown) {
    if (error instanceof ClientError && error.status === 401) {
      this.stop(); this.client.clear();
      Object.keys(sessionStorage).filter(key => key.startsWith('chat:create:')).forEach(key => sessionStorage.removeItem(key));
      this.unauthorized(); return;
    }
    this.update({ phase: 'failed', error: error instanceof Error ? error.message : 'Не удалось получить ответ' });
  }
  restore(userId: string) {
    try {
      const parsed = pendingSchema.safeParse(JSON.parse(sessionStorage.getItem(`chat:create:${userId}`) ?? 'null'));
      if (parsed.success) this.pending = parsed.data;
      return parsed.success ? parsed.data : null;
    } catch { return null; }
  }
  async recover(id: string) {
    if (this.abort.signal.aborted) return;
    this.update({ phase: 'recovering' });
    const signal = this.abort.signal, started = Date.now();
    try {
      while (!signal.aborted) {
        const history = await listMessages(id, signal);
        if (signal.aborted) return;
        // Retain partial text while an earlier persisted snapshot catches up.
        const merged = history.map(m => {
          const local = this.state.messages.find(x => x.id === m.id);
          return ['generating', 'interrupted'].includes(m.status) && local && local.text.length > m.text.length ? { ...m, text: local.text } : m;
        });
        if (history.length) this.update({ messages: merged });
        this.client.setQueryData(queryKeys.messages(id), history);
        if (this.pending && history.some(m => m.client_message_id === this.pending?.messageId)) this.pending = null;
        if (!history.some(m => m.status === 'generating')) {
          this.update({ phase: history.some(m => ['failed', 'interrupted'].includes(m.status)) ? 'failed' : 'idle',
            tools: {}, error: history.length ? '' : 'Сообщение не найдено. Можно явно повторить отправку.' });
          return;
        }
        if (Date.now() - started >= 130_000) throw new Error('Не удалось восстановить ответ. Обновите историю.');
        await new Promise<void>(resolve => {
          const finish = () => { clearTimeout(timer); signal.removeEventListener('abort', finish); resolve(); };
          const timer = setTimeout(finish, 2000); signal.addEventListener('abort', finish, { once: true });
        });
      }
    } catch (error) { if (!signal.aborted) this.fail(error); }
    finally { this.invalidate(); }
  }
  private invalidate() {
    for (const key of [queryKeys.conversations(), queryKeys.plan(), queryKeys.requests()]) void this.client.invalidateQueries({ queryKey: key });
  }
  async send(text: string, userId: string, stepId: string | null, navigate: (id: string) => void) {
    if (!text.trim() || text.length > 8000 || ['sending', 'streaming', 'recovering'].includes(this.state.phase)) return;
    const signal = this.abort.signal;
    if (signal.aborted) return;
    const pending = this.pending?.text === text ? this.pending : {
      text, stepId, conversationId: crypto.randomUUID(), messageId: crypto.randomUUID(),
    };
    this.pending = pending;
    this.update({ phase: 'sending', error: '', tools: {}, assistantId: null, userId: null });
    let id = this.state.conversationId;
    try {
      if (!id) {
        sessionStorage.setItem(`chat:create:${userId}`, JSON.stringify(pending));
        const conversation = await createConversation(pending.conversationId, pending.stepId, signal);
        if (signal.aborted) return;
        id = conversation.id;
        this.update({ conversationId: id });
        sessionStorage.removeItem(`chat:create:${userId}`);
        navigate(id);
      }
      // Reconcile an unknown outcome before an explicit repeat under the same UUID.
      const history = await listMessages(id, signal);
      if (history.some(m => m.client_message_id === pending.messageId) || history.some(m => m.status === 'generating')) {
        this.pending = null; await this.recover(id); return;
      }
      this.update({ messages: [...history, {
        id: pending.messageId, client_message_id: pending.messageId, in_reply_to_message_id: null,
        role: 'user', status: 'completed', text, sources: [], request_ids: [], created_at: new Date().toISOString(),
      }] });
      const response = await openApiStream(`/conversations/${encodeURIComponent(id)}/messages`, {
        method: 'POST', body: { client_message_id: pending.messageId, text }, signal,
      });
      this.update({ phase: 'streaming' });
      await readSse(response.body!, event => {
        if (signal.aborted) return;
        const next = applyEvent(this.state, event);
        if (event.event === 'action.proposed') {
          if (event.data.request.conversation_id !== id) throw protocolError();
          acceptAccessRequest(this.client, event.data.request);
        }
        this.update(next);
      });
      this.pending = null;
      this.client.setQueryData(queryKeys.messages(id), this.state.messages);
      this.update({ phase: this.state.messages.at(-1)?.status === 'failed' ? 'failed' : 'idle' });
    } catch (error) {
      if (signal.aborted) return;
      if (error instanceof ClientError && error.status === 401) { this.fail(error); return; }
      this.fail(error);
      if (id && !(error instanceof ClientError && error.code === 'IDEMPOTENCY_CONFLICT')) await this.recover(id);
    } finally { if (!signal.aborted) this.invalidate(); }
  }
}
