import { apiRequest } from '@/shared/api';
import { accessRequestSchema } from './schemas';

export const accessRequestsApi = {
  getAccessRequest(requestId: string, signal: AbortSignal) {
    return apiRequest(
      `/access-requests/${encodeURIComponent(requestId)}`,
      accessRequestSchema,
      { signal },
    );
  },
  // 202 (продолжает сверку) и 200 (окончательный результат) различаются по status ответа.
  confirmAccessRequest(requestId: string, draftVersion: number) {
    return apiRequest(
      `/access-requests/${encodeURIComponent(requestId)}/confirm`,
      accessRequestSchema,
      { method: 'POST', body: { draft_version: draftVersion } },
    );
  },
};
