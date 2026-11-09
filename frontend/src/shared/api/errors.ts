import { z } from 'zod';

export const apiErrorSchema = z.object({
  error: z.object({
    code: z.string(), message: z.string(), trace_id: z.string(),
    field_errors: z.record(z.string(), z.string()).optional(),
  }),
});

export type ClientErrorKind = 'http' | 'network' | 'protocol' | 'aborted';
type ErrorDetails = {
  status?: number; code?: string; traceId?: string;
  fieldErrors?: Record<string, string>; retryAfter?: string;
  cause?: unknown;
};

export class ClientError extends Error {
  readonly kind: ClientErrorKind;
  readonly status: number | undefined;
  readonly code: string | undefined;
  readonly traceId: string | undefined;
  readonly fieldErrors: Record<string, string> | undefined;
  readonly retryAfter: string | undefined;

  constructor(kind: ClientErrorKind, message: string, details: ErrorDetails = {}) {
    super(message, { cause: details.cause });
    this.name = 'ClientError';
    this.kind = kind;
    this.status = details.status;
    this.code = details.code;
    this.traceId = details.traceId;
    this.fieldErrors = details.fieldErrors;
    this.retryAfter = details.retryAfter;
  }
}
