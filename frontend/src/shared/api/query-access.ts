import { hashKey, type QueryClient, type QueryKey } from '@tanstack/react-query';
import { sessionSignal } from './session-signal';

const scopes = new WeakMap<QueryClient, Map<string, AbortController>>();

// Requests started before an access denial must never repopulate that cache,
// even if the transport ignores cancellation and returns a late success.
export function queryAccessSignal(client: QueryClient, key: QueryKey): AbortSignal {
  let entries = scopes.get(client);
  if (!entries) { entries = new Map(); scopes.set(client, entries); }
  const hash = hashKey(key);
  let scope = entries.get(hash);
  if (!scope) { scope = new AbortController(); entries.set(hash, scope); }
  return scope.signal;
}

export function revokeQueryAccess(client: QueryClient, key: QueryKey): void {
  const entries = scopes.get(client), hash = hashKey(key);
  entries?.get(hash)?.abort();
  entries?.delete(hash);
}

export function queryRequestSignal(client: QueryClient, key: QueryKey, signal?: AbortSignal): AbortSignal {
  return AbortSignal.any([sessionSignal(client), queryAccessSignal(client, key), ...(signal ? [signal] : [])]);
}
