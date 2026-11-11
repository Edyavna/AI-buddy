import type { QueryClient } from '@tanstack/react-query';

const sessions = new WeakMap<QueryClient, AbortController>();

export function sessionSignal(client: QueryClient): AbortSignal {
  let session = sessions.get(client);
  if (!session) {
    session = new AbortController();
    sessions.set(client, session);
  }
  return session.signal;
}

export function abortSessionRequests(client: QueryClient) {
  sessions.get(client)?.abort();
  sessions.delete(client);
}
