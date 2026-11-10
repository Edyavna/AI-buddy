import type { QueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/shared/api';
import type { AccessRequest } from './schemas';

export type AcceptResult = 'accepted' | 'ignored' | 'conflict';

// Shared accept path for HTTP responses and action.proposed stream events
// (integration.md §4). A stale event must never roll submitting/pending/granted
// back to draft: smaller revision is dropped, equal revision does not replace
// the record, greater revision wins. Equal revision with different content is a
// protocol conflict: the caller must GET the actual state.
export function acceptAccessRequest(
  queryClient: QueryClient,
  request: AccessRequest,
): AcceptResult {
  const key = queryKeys.request(request.id);
  const existing = queryClient.getQueryData<AccessRequest>(key);

  let result: AcceptResult;
  if (!existing || request.revision > existing.revision) {
    queryClient.setQueryData(key, request);
    result = 'accepted';
  } else if (request.revision < existing.revision) {
    result = 'ignored';
  } else if (JSON.stringify(existing) === JSON.stringify(request)) {
    result = 'ignored';
  } else {
    result = 'conflict';
  }

  if (result !== 'ignored') {
    void queryClient.invalidateQueries({ queryKey: queryKeys.requests() });
  }
  if (result === 'conflict') {
    void queryClient.invalidateQueries({ queryKey: key });
  }
  return result;
}
