import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createHandlers } from '@/mocks/handlers';
import { createScenario } from '@/mocks/fixtures/scenario';
import { queryKeys, createQueryClient } from '@/shared/api';
import { accessRequestSchema } from '@/features/access-requests/schemas';
import type { AccessRequest } from '@/features/access-requests/schemas';
import { AccessRequestPanel, acceptAccessRequest } from '@/features/access-requests';

function seedRequestCache(queryClient: ReturnType<typeof createQueryClient>, request: AccessRequest) {
  queryClient.setQueryData(queryKeys.request(request.id), request);
}

describe('AccessRequestPanel (#4 — read path)', () => {
  it('loads the request and renders status, resource and reason', async () => {
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    expect(await screen.findByText('Тестовый репозиторий')).toBeInTheDocument();
    expect(screen.getByText('Черновик')).toBeInTheDocument();
    expect(screen.getByText('Подготовить локальное окружение')).toBeInTheDocument();
    expect(screen.getByText('Право: чтение')).toBeInTheDocument();
  });

  it('shows the external id and check time for a pending request', async () => {
    const f = createScenario();
    server.resetHandlers(...[
      http.get('*/api/v1/access-requests/:id', () =>
        HttpResponse.json(accessRequestSchema.parse(f.pending))),
    ], ...createHandlers());
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    expect(await screen.findByText('Номер: ACCESS-1001')).toBeInTheDocument();
    expect(screen.getByText(/Проверено:/)).toBeInTheDocument();
    expect(screen.getAllByText('Ожидает согласования').length).toBeGreaterThan(0);
  });

  it('renders a neutral explanation when failed has no failure_reason', async () => {
    const f = createScenario();
    const failed = accessRequestSchema.parse({ ...f.draft, status: 'failed', external_id: null });
    server.resetHandlers(...[
      http.get('*/api/v1/access-requests/:id', () => HttpResponse.json(failed)),
    ], ...createHandlers());
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    expect(await screen.findByText('Не удалось отправить заявку')).toBeInTheDocument();
  });

  it('shows a not-found state for an unknown request id', async () => {
    renderWithQuery(<AccessRequestPanel requestId="request_missing" />);
    expect(await screen.findByText('Заявка недоступна или удалена')).toBeInTheDocument();
  });

  it('shows a retryable error on server failure', async () => {
    server.resetHandlers(...[
      http.get('*/api/v1/access-requests/:id', () => HttpResponse.json(
        { error: { code: 'INTERNAL', message: 'Сервис заявок недоступен', trace_id: 'trace_mock' } },
        { status: 503 },
      )),
    ], ...createHandlers());
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Сервис заявок недоступен');
    expect(screen.getByRole('button', { name: /повторить/i })).toBeInTheDocument();
  });
});

describe('acceptAccessRequest (#4 — shared accept path for FE1)', () => {
  it('accepts a newer revision into the shared cache', () => {
    const f = createScenario();
    const queryClient = createQueryClient();
    const draft = accessRequestSchema.parse(f.draft);
    seedRequestCache(queryClient, draft);
    const pending = accessRequestSchema.parse(f.pending);

    expect(acceptAccessRequest(queryClient, pending)).toBe('accepted');
    expect(queryClient.getQueryData<AccessRequest>(queryKeys.request(draft.id))?.status).toBe('pending');
  });

  it('ignores a stale event so granted access never rolls back to draft', () => {
    const f = createScenario();
    const queryClient = createQueryClient();
    seedRequestCache(queryClient, accessRequestSchema.parse(f.granted));

    expect(acceptAccessRequest(queryClient, accessRequestSchema.parse(f.draft))).toBe('ignored');
    expect(queryClient.getQueryData<AccessRequest>(queryKeys.request(f.granted.id))?.status).toBe('granted');
  });

  it('does not replace the record on an equal revision', () => {
    const f = createScenario();
    const queryClient = createQueryClient();
    const pending = accessRequestSchema.parse(f.pending);
    seedRequestCache(queryClient, pending);

    expect(acceptAccessRequest(queryClient, pending)).toBe('ignored');
    expect(queryClient.getQueryData(queryKeys.request(pending.id))).toBe(pending);
  });

  it('reports a protocol conflict on equal revision with different content', () => {
    const f = createScenario();
    const queryClient = createQueryClient();
    const pending = accessRequestSchema.parse(f.pending);
    seedRequestCache(queryClient, pending);
    const contradictory = accessRequestSchema.parse({ ...f.pending, draft_version: 2 });

    expect(acceptAccessRequest(queryClient, contradictory)).toBe('conflict');
  });
});
