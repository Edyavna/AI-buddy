import { expect, it } from 'vitest';
import { createQueryClient, ClientError, queryKeys } from './index';

it('isolates clients, uses stable keys and limits safe query retries', async () => {
  const client = createQueryClient();
  client.setQueryData(queryKeys.me(), { id: 'one' });
  expect(client.getQueryData(['session', 'me'])).toEqual({ id: 'one' });
  expect(createQueryClient().getQueryData(queryKeys.me())).toBeUndefined();
  expect(queryKeys.plan()).toEqual(['onboarding', 'plan']);
  expect(queryKeys.conversations()).toEqual(['chat', 'conversations']);
  expect(queryKeys.conversation('1')).toEqual(['chat', 'conversation', '1']);
  expect(queryKeys.messages('1')).toEqual(['chat', 'messages', '1']);
  expect(queryKeys.document('1')).toEqual(['documents', '1']);
  expect(queryKeys.requests()).toEqual(['access-requests', 'list']);
  expect(queryKeys.request('1')).toEqual(['access-requests', 'detail', '1']);
  const retry = client.getDefaultOptions().queries?.retry;
  if (typeof retry !== 'function') throw new Error('Missing query retry policy');
  expect(retry(0, new ClientError('network', 'offline'))).toBe(true);
  expect(retry(0, new ClientError('http', 'busy', { status: 503 }))).toBe(true);
  for (const error of [new ClientError('http', 'auth', { status: 401 }), new ClientError('aborted', 'cancel'), new ClientError('protocol', 'bad'), new Error('unknown')]) {
    expect(retry(0, error)).toBe(false);
  }
  expect(retry(2, new ClientError('network', 'offline'))).toBe(false);
  let calls = 0;
  const mutation = client.getMutationCache().build(client, { mutationFn: async () => { calls++; throw new Error('unknown outcome'); } });
  await expect(mutation.execute(undefined)).rejects.toThrow('unknown outcome');
  expect(calls).toBe(1);
  client.clear();
});
