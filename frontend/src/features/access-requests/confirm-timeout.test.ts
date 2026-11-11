import { QueryClient } from '@tanstack/react-query';
import { HttpResponse, http } from 'msw';
import { afterEach, expect, it, vi } from 'vitest';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { queryKeys } from '@/shared/api';
import { confirmAccessRequestShared } from './confirm';
import { accessRequestSchema } from './schemas';

afterEach(() => vi.restoreAllMocks());

it('reconciles a timed-out confirm before unlocking the latest draft version', async () => {
  const client = new QueryClient();
  const draft = accessRequestSchema.parse(createScenario().draft);
  const timeout = new AbortController();
  const realTimeout = AbortSignal.timeout;
  vi.spyOn(AbortSignal, 'timeout').mockImplementationOnce(() => timeout.signal).mockImplementation(realTimeout);
  const versions: number[] = [];
  const current = { ...draft, draft_version: draft.draft_version + 1, revision: draft.revision + 1 };
  let notifyPost!: () => void;
  const started = new Promise<void>(resolve => { notifyPost = resolve; });
  server.use(
    http.get('*/api/v1/access-requests/:id', () => HttpResponse.json(current)),
    http.post('*/api/v1/access-requests/:id/confirm', async ({ request }) => {
      const { draft_version } = await request.json() as { draft_version: number };
      versions.push(draft_version);
      if (versions.length === 1) {
        notifyPost();
        await new Promise<void>(resolve => request.signal.addEventListener('abort', () => resolve(), { once: true }));
        return HttpResponse.error();
      }
      return HttpResponse.json({ ...current, status: 'submitting', revision: current.revision + 1 });
    }),
  );

  const first = confirmAccessRequestShared(client, draft.id, draft.draft_version);
  expect(confirmAccessRequestShared(client, draft.id, draft.draft_version)).toBe(first);
  const reconciled = expect(first).rejects.toMatchObject({ code: 'DRAFT_CHANGED' });
  await started;
  timeout.abort();
  await reconciled;
  expect(client.getQueryData(queryKeys.request(draft.id))).toEqual(current);
  await expect(confirmAccessRequestShared(client, draft.id, current.draft_version)).resolves.toMatchObject({ status: 'submitting' });
  expect(versions).toEqual([draft.draft_version, current.draft_version]);
  client.clear();
});

it('accepts the persisted status when the server confirms but the POST response is lost', async () => {
  const client = new QueryClient();
  const scenario = createScenario();
  const draft = accessRequestSchema.parse(scenario.draft);
  const submitted = accessRequestSchema.parse(scenario.submitting);
  const calls: string[] = [];
  server.use(
    http.post('*/api/v1/access-requests/:id/confirm', () => {
      calls.push('POST');
      return HttpResponse.error();
    }),
    http.get('*/api/v1/access-requests/:id', () => {
      calls.push('GET');
      return HttpResponse.json(submitted);
    }),
  );
  await expect(confirmAccessRequestShared(client, draft.id, draft.draft_version)).resolves.toEqual(submitted);
  expect(client.getQueryData(queryKeys.request(draft.id))).toEqual(submitted);
  expect(calls).toEqual(['POST', 'GET']);
  client.clear();
});
