import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse, delay } from 'msw';
import { z } from 'zod';
import { apiRequest, openApiStream, ClientError } from './index';
import { server } from '../../test/server';

const schema = z.object({ id: z.string() });
const endpoint = '*/api/v1/probe';

describe('JSON transport', () => {
  it('sends same-origin cookies, JSON, method and Accept; preserves 202', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    server.use(http.post(endpoint, async ({ request }) => {
      expect(request.headers.get('content-type')).toBe('application/json');
      expect(request.headers.get('accept')).toBe('application/json');
      expect(await request.json()).toEqual({ draft_version: 1 });
      return HttpResponse.json({ id: 'request_demo_1' }, { status: 202 });
    }));
    expect(await apiRequest('/probe', schema, { method: 'POST', body: { draft_version: 1 } }))
      .toEqual({ status: 202, data: { id: 'request_demo_1' } });
    expect(fetchSpy.mock.calls[0]?.[1]?.credentials).toBe('same-origin');
    fetchSpy.mockRestore();
  });

  it('does not parse a 204 response', async () => {
    server.use(http.delete(endpoint, () => new HttpResponse(null, { status: 204 })));
    expect(await apiRequest('/probe', z.never(), { method: 'DELETE' })).toEqual({ status: 204, data: undefined });
  });

  it.each([401, 409, 503, 422, 429])('preserves HTTP %s and structured error metadata', async status => {
    server.use(http.get(endpoint, () => HttpResponse.json({ error: {
      code: 'TEST_ERROR', message: 'Тестовая ошибка', trace_id: 'trace_1', field_errors: { text: 'Ошибка поля' },
    } }, { status, headers: { 'Retry-After': '5' } })));
    await expect(apiRequest('/probe', schema)).rejects.toMatchObject({
      kind: 'http', status, code: 'TEST_ERROR', traceId: 'trace_1',
      fieldErrors: { text: 'Ошибка поля' }, retryAfter: '5',
    });
  });

  it.each([
    () => new HttpResponse('<h1>Bad gateway</h1>', { status: 503, headers: { 'Content-Type': 'text/html' } }),
    () => HttpResponse.json({ unexpected: true }, { status: 503 }),
    () => new HttpResponse('{', { status: 503, headers: { 'Content-Type': 'application/json' } }),
  ])('normalizes proxy/malformed errors without exposing response bodies', async response => {
    server.use(http.get(endpoint, response));
    await expect(apiRequest('/probe', schema)).rejects.toMatchObject({ kind: 'http', status: 503, message: 'Ошибка сервера (503)' });
  });

  it.each([
    () => new HttpResponse('html', { headers: { 'Content-Type': 'text/html' } }),
    () => new HttpResponse('{', { headers: { 'Content-Type': 'application/json' } }),
    () => HttpResponse.json({ id: 3 }),
  ])('rejects invalid successful responses as protocol errors', async response => {
    server.use(http.get(endpoint, response));
    await expect(apiRequest('/probe', schema)).rejects.toMatchObject({ kind: 'protocol', status: 200 });
  });

  it('accepts JSON media types with suffix and charset', async () => {
    server.use(http.get(endpoint, () => new HttpResponse('{"id":"ok"}', { headers: { 'Content-Type': 'application/problem+json; charset=utf-8' } })));
    expect((await apiRequest('/probe', schema)).data).toEqual({ id: 'ok' });
  });

  it('reports network failure and never retries a mutation', async () => {
    let calls = 0;
    server.use(http.post(endpoint, () => { calls++; return HttpResponse.error(); }));
    await expect(apiRequest('/probe', schema, { method: 'POST' })).rejects.toBeInstanceOf(ClientError);
    expect(calls).toBe(1);
    server.use(http.get(endpoint, () => HttpResponse.error()));
    await expect(apiRequest('/probe', schema)).rejects.toMatchObject({ kind: 'network' });
  });

  it('supports AbortSignal before and during the request', async () => {
    server.use(http.get(endpoint, async () => { await delay(50); return HttpResponse.json({ id: 'late' }); }));
    const controller = new AbortController();
    const request = apiRequest('/probe', schema, { signal: controller.signal });
    controller.abort();
    await expect(request).rejects.toMatchObject({ kind: 'aborted' });
    await expect(apiRequest('/probe', schema, { signal: controller.signal })).rejects.toMatchObject({ kind: 'aborted' });
  });

  it.each(['https://elsewhere.test/', '//elsewhere.test', '/../secrets', '/x?redirect=y'])('rejects unsafe API path %s', async path => {
    await expect(apiRequest(path, schema)).rejects.toMatchObject({ kind: 'protocol' });
  });
});

describe('stream opening', () => {
  it('returns raw SSE separately from JSON parsing', async () => {
    server.use(http.post(endpoint, ({ request }) => {
      expect(request.headers.get('accept')).toBe('text/event-stream');
      return new HttpResponse(': heartbeat\n\n', { headers: { 'Content-Type': 'text/event-stream; charset=utf-8' } });
    }));
    const response = await openApiStream('/probe', { method: 'POST', body: { text: 'Вопрос' } });
    expect(response.status).toBe(200);
    expect(await response.text()).toBe(': heartbeat\n\n');
  });
  it('rejects JSON instead of a stream', async () => {
    server.use(http.get(endpoint, () => HttpResponse.json({ id: 'ok' })));
    await expect(openApiStream('/probe')).rejects.toMatchObject({ kind: 'protocol', status: 200 });
  });
  it('preserves errors before the stream opens', async () => {
    server.use(http.get(endpoint, () => HttpResponse.json({ error: { code: 'GENERATION_IN_PROGRESS', message: 'Подождите', trace_id: 't' } }, { status: 409 })));
    await expect(openApiStream('/probe')).rejects.toMatchObject({ status: 409, code: 'GENERATION_IN_PROGRESS' });
  });
});
