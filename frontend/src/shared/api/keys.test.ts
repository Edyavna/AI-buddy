import { describe, it, expect } from 'vitest';
import { queryKeys } from './keys';

describe('queryKeys', () => {
  it('generates consistent key tuples', () => {
    expect(queryKeys.me()).toEqual(['session', 'me']);
    expect(queryKeys.plan()).toEqual(['onboarding', 'plan']);
    expect(queryKeys.conversations()).toEqual(['chat', 'conversations']);
    expect(queryKeys.conversation('conv-1')).toEqual(['chat', 'conversation', 'conv-1']);
    expect(queryKeys.messages('conv-1')).toEqual(['chat', 'messages', 'conv-1']);
    expect(queryKeys.document('doc-1')).toEqual(['documents', 'doc-1']);
    expect(queryKeys.requests()).toEqual(['access-requests', 'list']);
    expect(queryKeys.request('req-1')).toEqual(['access-requests', 'detail', 'req-1']);
  });
});
