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
};
