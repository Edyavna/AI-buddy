import type { z } from 'zod';
import { apiErrorSchema, ClientError } from './errors';

export const API_BASE = '/api/v1';
export type RequestOptions = {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
  search?: Record<string, string | undefined>;
};
export type ApiResponse<T> = { status: number; data: T | undefined };

function normalize(error: unknown, signal?: AbortSignal): ClientError {
  if (signal?.aborted || (error instanceof Error && error.name === 'AbortError')) {
    return new ClientError('aborted', 'Запрос отменён', { cause: error });
  }
  if (error instanceof ClientError) return error;
  return new ClientError('network', 'Не удалось связаться с сервером', { cause: error });
}

const isJson = (response: Response) => {
  const type = response.headers.get('content-type')?.split(';')[0]?.trim();
  return type === 'application/json' || Boolean(type?.endsWith('+json'));
};

async function readJson(response: Response): Promise<unknown> {
  if (!isJson(response)) {
    throw new ClientError('protocol', 'Сервер вернул неожиданный формат ответа', { status: response.status });
  }
  try { return await response.json(); }
  catch (cause) {
    if (cause instanceof Error && cause.name === 'AbortError') throw cause;
    throw new ClientError('protocol', 'Сервер вернул некорректный JSON', { status: response.status, cause });
  }
}

async function checkHttp(response: Response) {
  if (response.ok) return;
  const retryAfter = response.headers.get('retry-after');
  let data: unknown;
  try { data = await readJson(response); }
  catch (cause) {
    if (cause instanceof Error && cause.name === 'AbortError') throw cause;
    // Preserve HTTP status even for an HTML error from the proxy.
    throw new ClientError('http', `Ошибка сервера (${response.status})`, {
      status: response.status, cause, ...(retryAfter && { retryAfter }),
    });
  }
  const parsed = apiErrorSchema.safeParse(data);
  const error = parsed.success ? parsed.data.error : undefined;
  throw new ClientError('http', error?.message ?? `Ошибка сервера (${response.status})`, {
    status: response.status,
    ...(error && { code: error.code, traceId: error.trace_id }),
    ...(error?.field_errors && { fieldErrors: error.field_errors }),
    ...(retryAfter && { retryAfter }),
  });
}

async function send(path: string, options: RequestOptions, accept: string) {
  // Keep session cookies and requests within our API; encode dynamic path segments at call sites.
  if (!path.startsWith('/') || path.startsWith('//') || /[?#\\]|(?:^|\/)\.\.(?:\/|$)/.test(path)) {
    throw new ClientError('protocol', 'Некорректный путь API');
  }
  try {
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(options.search ?? {})) if (value !== undefined) search.set(key, value);
    const query = search.size ? `?${search}` : '';
    const multipart = options.body instanceof FormData;
    const response = await fetch(`${API_BASE}${path}${query}`, {
      method: options.method ?? 'GET', credentials: 'same-origin',
      headers: { Accept: accept, ...(options.body !== undefined && !multipart && { 'Content-Type': 'application/json' }) },
      ...(options.body !== undefined && { body: multipart ? options.body as FormData : JSON.stringify(options.body) }),
      ...(options.signal && { signal: options.signal }),
    });
    await checkHttp(response);
    return response;
  } catch (error) { throw normalize(error, options.signal); }
}

// Schema belongs to the consuming feature. 204 explicitly returns undefined.
export async function apiRequest<T>(path: string, schema: z.ZodType<T>, options: RequestOptions = {}): Promise<ApiResponse<T>> {
  try {
    const response = await send(path, options, 'application/json');
    if (response.status === 204) return { status: 204, data: undefined };
    const parsed = schema.safeParse(await readJson(response));
    if (!parsed.success) throw new ClientError('protocol', 'Ответ сервера не соответствует контракту', {
      status: response.status, cause: parsed.error,
    });
    return { status: response.status, data: parsed.data };
  } catch (error) { throw normalize(error, options.signal); }
}

// Raw stream stays separate from JSON. SSE reader/recovery will be implemented in #3.
export async function openApiStream(path: string, options: RequestOptions = {}): Promise<Response> {
  const response = await send(path, options, 'text/event-stream');
  if (response.headers.get('content-type')?.split(';')[0]?.trim() !== 'text/event-stream' || !response.body) {
    throw new ClientError('protocol', 'Сервер не открыл поток ответа', { status: response.status });
  }
  return response;
}
