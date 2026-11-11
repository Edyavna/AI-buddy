import { z } from 'zod';
import { ClientError } from '@/shared/api';
import { messageSchema } from './schemas';
import { accessRequestSchema } from '@/features/access-requests';

const id = z.string().min(1);
export const eventSchema = z.discriminatedUnion('event', [
  z.object({ event: z.literal('message.started'), data: z.object({ user_message_id: id, message_id: id }) }),
  z.object({ event: z.literal('tool.started'), data: z.object({ message_id: id, tool_call_id: id, label: id }) }),
  z.object({ event: z.literal('tool.finished'), data: z.object({ message_id: id, tool_call_id: id, status: z.enum(['succeeded', 'failed']) }) }),
  z.object({ event: z.literal('answer.delta'), data: z.object({ message_id: id, text: z.string() }) }),
  z.object({ event: z.literal('action.proposed'), data: z.object({ message_id: id, request: accessRequestSchema.refine(r => r.status === 'draft' && r.external_id === null) }) }),
  z.object({ event: z.literal('message.completed'), data: z.object({ message: messageSchema.refine(m => m.role === 'assistant' && m.status === 'completed') }) }),
  z.object({ event: z.literal('message.failed'), data: z.object({ message: messageSchema.refine(m => m.role === 'assistant' && m.status === 'failed') }) }),
]);
export type ChatEvent = z.infer<typeof eventSchema>;
const known: Set<string> = new Set(eventSchema.options.map(option => option.shape.event.value));
export const protocolError = () => new ClientError('protocol', 'Нарушен протокол ответа. Обновите историю.');

// Streaming decoder retains incomplete UTF-8; framing retains incomplete lines/JSON.
export async function readSse(body: ReadableStream<Uint8Array>, onEvent: (event: ChatEvent) => void) {
  const reader = body.getReader();
  const decoder = new TextDecoder('utf-8', { fatal: true });
  let buffer = '', event = '', data: string[] = [];
  let terminal = false;
  function line(value: string) {
    if (!value) {
      if (known.has(event)) {
        if (terminal) throw protocolError();
        let json: unknown;
        try { json = JSON.parse(data.join('\n')); } catch { throw protocolError(); }
        const result = eventSchema.safeParse({ event, data: json });
        if (!result.success) throw protocolError();
        onEvent(result.data);
        terminal = event === 'message.completed' || event === 'message.failed';
      }
      event = ''; data = []; return;
    }
    if (value.startsWith(':')) return;
    const colon = value.indexOf(':');
    const key = colon < 0 ? value : value.slice(0, colon);
    const content = colon < 0 ? '' : value.slice(colon + 1).replace(/^ /, '');
    if (key === 'event') event = content;
    if (key === 'data') data.push(content);
  }
  try {
    while (!terminal) {
      const { value, done } = await reader.read();
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true });
      let end: number;
      while ((end = buffer.indexOf('\n')) >= 0) {
        line(buffer.slice(0, end).replace(/\r$/, ''));
        buffer = buffer.slice(end + 1);
      }
      if (done) break;
    }
    if (!terminal) throw protocolError();
  } finally {
    void reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

export type Message = z.infer<typeof messageSchema>;
export type StreamState = { messages: Message[]; tools: Record<string, string>; assistantId: string | null; userId: string | null };
export function applyEvent(state: StreamState, event: ChatEvent): StreamState {
  if (event.event === 'message.started') {
    if (state.assistantId) throw protocolError();
    const user = state.messages.at(-1);
    if (!user || user.role !== 'user') throw protocolError();
    return { ...state, userId: event.data.user_message_id, assistantId: event.data.message_id,
      messages: [...state.messages.slice(0, -1), { ...user, id: event.data.user_message_id }, {
        id: event.data.message_id, role: 'assistant', status: 'generating', text: '',
        client_message_id: null, in_reply_to_message_id: event.data.user_message_id,
        sources: [], request_ids: [], created_at: user.created_at,
      }] };
  }
  const target = 'message' in event.data ? event.data.message.id : event.data.message_id;
  if (!state.assistantId || state.assistantId !== target) throw protocolError();
  switch (event.event) {
    case 'tool.started': return { ...state, tools: { ...state.tools, [event.data.tool_call_id]: event.data.label } };
    case 'tool.finished': {
      const tools = { ...state.tools };
      if (!tools[event.data.tool_call_id]) throw protocolError();
      tools[event.data.tool_call_id] += event.data.status === 'failed' ? ' — не удалось' : ' — готово';
      return { ...state, tools };
    }
    case 'answer.delta': return { ...state, messages: state.messages.map(m => m.id === target ? { ...m, text: m.text + event.data.text } : m) };
    case 'action.proposed': return { ...state, messages: state.messages.map(m => m.id === target ? { ...m, request_ids: [...new Set([...m.request_ids, event.data.request.id])] } : m) };
    default:
      if (event.data.message.in_reply_to_message_id !== state.userId) throw protocolError();
      return { ...state, messages: state.messages.map(m => m.id === target ? event.data.message : m), tools: {} };
  }
}
