import { expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { useQuery } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { z } from 'zod';
import { apiRequest, queryKeys } from '../../shared/api';
import { createHandlers } from './index';
import { createScenario } from '../fixtures/scenario';
import { conversationFixtureSchema, userFixtureSchema, requestFixtureSchema, messageFixtureSchema, documentFixtureSchema } from '../fixtures/schemas';
import { server } from '../../test/server';
import { renderWithQuery } from '../../test/render';
import { startBrowserMocks } from '../browser';

it('provides idempotent conversation creation and rejects changed parameters', async () => {
  const body = { client_conversation_id: '33333333-3333-4333-8333-333333333333' };
  const first = await apiRequest('/conversations', conversationFixtureSchema, { method: 'POST', body });
  expect(first.status).toBe(201);
  const repeated = await apiRequest('/conversations', conversationFixtureSchema, { method: 'POST', body });
  expect(repeated.status).toBe(200);
  expect(repeated.data).toEqual(first.data);
  await expect(apiRequest('/conversations', conversationFixtureSchema, { method: 'POST', body: { ...body, step_id: 'changed' } })).rejects.toMatchObject({ code: 'IDEMPOTENCY_CONFLICT' });
  const list = await apiRequest('/conversations', z.array(conversationFixtureSchema));
  expect(list.data).toHaveLength(2);
  expect((await apiRequest(`/conversations/${first.data?.id}`, conversationFixtureSchema)).data).toEqual(first.data);
  await expect(apiRequest('/conversations/missing', conversationFixtureSchema)).rejects.toMatchObject({ status: 404 });
});

it('lets FE2 provide GET and confirm handlers using the shared transport', async () => {
  const f = createScenario();
  let request = requestFixtureSchema.parse(f.draft);
  server.resetHandlers(...createHandlers([
    http.get('*/api/v1/me', () => HttpResponse.json(f.user)),
    http.post('*/api/v1/access-requests/:id/confirm', async ({ request: incoming }) => {
      expect(await incoming.json()).toEqual({ draft_version: f.draft.draft_version });
      request = requestFixtureSchema.parse(f.pending);
      return HttpResponse.json(request, { status: 202 });
    }),
    http.get('*/api/v1/access-requests/:id', () => HttpResponse.json(request)),
  ]));
  function Profile() {
    const query = useQuery({ queryKey: queryKeys.me(), queryFn: async ({ signal }) =>
      (await apiRequest('/me', userFixtureSchema, { signal })).data });
    return <p>{query.data?.name ?? 'Загрузка'}</p>;
  }
  renderWithQuery(<Profile />);
  expect(await screen.findByText(f.user.name)).toBeInTheDocument();
  const result = await apiRequest(`/access-requests/${request.id}/confirm`, requestFixtureSchema, {
    method: 'POST', body: { draft_version: request.draft_version },
  });
  expect(result.status).toBe(202);
  expect(result.data?.external_id).toBe(f.pending.external_id);
  expect((await apiRequest(`/access-requests/${request.id}`, requestFixtureSchema)).data?.status).toBe('pending');
});

it('never activates browser mocks in production or without explicit opt-in', async () => {
  expect(await startBrowserMocks({ enabled: true, development: false })).toBeUndefined();
  expect(await startBrowserMocks({ enabled: false, development: true })).toBeUndefined();
});

it('loads canonical history and sources, reports inaccessible objects', async () => {
  const f = createScenario();
  const messages = await apiRequest(`/conversations/${f.conversation.id}/messages`, z.array(messageFixtureSchema));
  expect(messages.data).toEqual(f.messages);
  await expect(apiRequest('/conversations/missing/messages', z.array(messageFixtureSchema))).rejects.toMatchObject({ status: 404 });
  const source = f.messages.flatMap(message => message.sources)[0]!;
  const document = await apiRequest(`/documents/${source.document_id}`, documentFixtureSchema);
  expect(document.data?.title).toBe(source.title);
  await expect(apiRequest('/documents/missing', documentFixtureSchema)).rejects.toMatchObject({ status: 404 });
  await expect(apiRequest('/conversations', conversationFixtureSchema, { method: 'POST', body: {} })).rejects.toMatchObject({ status: 422 });
});
