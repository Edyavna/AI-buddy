import { expect, it } from 'vitest';
import { createScenario } from './scenario';
import { conversationFixtureSchema, userFixtureSchema, messageFixtureSchema, requestFixtureSchema, planFixtureSchema } from './schemas';
import type { AccessRequest, Conversation, Message, Plan, User } from '../../shared/api';

it('validates canonical fixtures and links without maintaining separate JSON copies', () => {
  const f = createScenario();
  const user: User = userFixtureSchema.parse(f.user);
  const conversation: Conversation = conversationFixtureSchema.parse(f.conversation);
  expect(user.id).toBe('intern_demo_1');
  expect(conversation.client_conversation_id).toBe(f.create_conversation.client_conversation_id);
  for (const item of f.messages) {
    const message = messageFixtureSchema.parse(item);
    // Zod optional outputs allow undefined; verify required generated fields without a cast.
    const required: Omit<Message, 'error'> = message;
    expect(required.id).toBeTruthy();
  }
  for (const item of [f.draft, f.submitting, f.pending, f.granted]) {
    const request = requestFixtureSchema.parse(item);
    const required: Omit<AccessRequest, 'failure_reason'> = request;
    expect(required.conversation_id).toBe(conversation.id);
  }
  for (const item of [f.plan, f.blocked_plan, f.granted_plan, f.completed_plan]) {
    const plan: Plan = planFixtureSchema.parse(item);
    expect(plan.steps[0]?.id).toBe(conversation.step_id);
  }
  expect(messageFixtureSchema.safeParse({ ...f.messages[1], status: 'failed' }).success).toBe(false);
  expect(requestFixtureSchema.safeParse({ ...f.pending, external_id: null }).success).toBe(false);
  expect(createScenario()).toEqual(f);
  f.user.name = 'changed';
  expect(createScenario().user.name).not.toBe('changed');
});
