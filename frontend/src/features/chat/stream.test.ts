import { describe, it, expect } from 'vitest';
import { createScenario } from '@/mocks/fixtures/scenario';
import { applyEvent, eventSchema, readSse, type StreamState } from './stream';
import { messageSchema } from './schemas';

const f = createScenario();
import { eventText, bytes } from '@/test/chat-stream';
const complete = eventText('message.completed', { message: f.messages[1] });
describe('SSE framing', () => {
  it.each(['\n', '\r\n'])('handles UTF-8/JSON split at every byte with %j', async newline => {
    const events: string[] = [];
    await readSse(bytes((': heartbeat\n\nevent: unknown\ndata: broken\n\n' + eventText('answer.delta', { message_id: 'msg_a1', text: 'Привет 🌍' }) + complete).replaceAll('\n', newline), true), e => events.push(e.event));
    expect(events).toEqual(['answer.delta', 'message.completed']);
  });
  it('handles multiline data', async () => {
    await expect(readSse(bytes('event: message.completed\ndata: {"message":\ndata: ' + JSON.stringify(f.messages[1]) + '}\n\n'), () => {})).resolves.toBeUndefined();
  });
  it.each(['', ': heartbeat\n\n', 'event: answer.delta\ndata: {"message_id":3}\n\n', 'event: answer.delta\ndata: {\n\n', complete + complete])('rejects incomplete or malformed known events', async text => {
    await expect(readSse(bytes(text), () => {})).rejects.toThrow();
  });
  it('validates action and terminal cross-field constraints', () => {
    expect(eventSchema.safeParse({ event: 'action.proposed', data: { message_id: 'm', request: f.pending } }).success).toBe(false);
    expect(eventSchema.safeParse({ event: 'message.completed', data: { message: f.failed_event.data.message } }).success).toBe(false);
  });
});

it('reduces started, tools, deltas, action and authoritative final without duplicates', () => {
  let state: StreamState = { messages: [messageSchema.parse(f.messages[0])], tools: {}, assistantId: null, userId: null };
  for (const raw of f.events) state = applyEvent(state, eventSchema.parse(raw));
  expect(state.messages).toEqual(f.messages);
  expect(state.tools).toEqual({});
  expect(() => applyEvent(state, eventSchema.parse(f.events[0]))).toThrow();
  expect(() => applyEvent({ ...state, assistantId: 'other' }, eventSchema.parse(f.events.at(-1)))).toThrow();
  expect(() => applyEvent(state, eventSchema.parse({ event: 'tool.finished', data: { message_id: 'msg_a1', tool_call_id: 'missing', status: 'failed' } }))).toThrow();
});
