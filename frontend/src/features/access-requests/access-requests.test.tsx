import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createHandlers } from '@/mocks/handlers';
import { createScenario } from '@/mocks/fixtures/scenario';
import { queryKeys, createQueryClient } from '@/shared/api';
import { accessRequestSchema } from '@/features/access-requests/schemas';
import type { AccessRequest } from '@/features/access-requests/schemas';
import { AccessRequestPanel, acceptAccessRequest } from '@/features/access-requests';

const REQUEST_URL = '*/api/v1/access-requests/:id';
const CONFIRM_URL = '*/api/v1/access-requests/:id/confirm';

function parseRequest(input: unknown): AccessRequest {
  return accessRequestSchema.parse(input);
}

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

describe('acceptAccessRequest (#4 — shared accept path for FE1)', () => {  it('accepts a newer revision into the shared cache', () => {
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


describe('AccessRequestPanel (#5 — confirm flow)', () => {
  it('sends the displayed draft_version on confirm and accepts a 202 submitting response', async () => {
    const f = createScenario();
    const bodies: unknown[] = [];
    let current = parseRequest(f.draft);
    server.use(
      http.get(REQUEST_URL, () => HttpResponse.json(current)),
      http.post(CONFIRM_URL, async ({ request }) => {
        bodies.push(await request.json());
        current = parseRequest(f.submitting);
        return HttpResponse.json(current, { status: 202 });
      }),
    );

    const user = userEvent.setup();
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    const confirmButton = await screen.findByRole('button', { name: 'Подтвердить отправку' });
    await user.click(confirmButton);

    // 202 — не успех: карточка в отправке, повторное подтверждение недоступно.
    expect(await screen.findByText('Отправляем заявку / уточняем результат')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Подтвердить отправку' })).not.toBeInTheDocument();
    expect(bodies).toEqual([{ draft_version: 1 }]);
  });

  it('keeps one in-flight confirm per requestId across two mounted cards', async () => {
    const f = createScenario();
    const bodies: unknown[] = [];
    let current = parseRequest(f.draft);
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get(REQUEST_URL, () => HttpResponse.json(current)),
      http.post(CONFIRM_URL, async ({ request }) => {
        bodies.push(await request.json());
        await gate; // исход неизвестен, пока тест не отпустит
        current = parseRequest(f.submitting);
        return HttpResponse.json(current, { status: 202 });
      }),
    );

    try {
      const user = userEvent.setup();
      renderWithQuery(
        <>
          <AccessRequestPanel requestId="request_demo_1" />
          <AccessRequestPanel requestId="request_demo_1" />
        </>,
      );
      const buttons = await screen.findAllByRole('button', { name: 'Подтвердить отправку' });
      const [firstButton, secondButton] = buttons;
      if (!firstButton || !secondButton) throw new Error('Ожидались две карточки заявки');
      await user.click(firstButton);
      await user.click(secondButton);

      // Две копии карточки — ровно одна сетевая мутация confirm.
      expect(bodies).toHaveLength(1);
    } finally {
      release();
    }

    await waitFor(() => {
      expect(screen.getAllByText('Отправляем заявку / уточняем результат').length).toBe(2);
    });
  });

  it('treats 409 DRAFT_CHANGED as a new draft requiring a fresh click with the new version', async () => {
    const f = createScenario();
    const bodies: unknown[] = [];
    let current = parseRequest(f.draft);
    server.use(
      http.get(REQUEST_URL, () => HttpResponse.json(current)),
      http.post(CONFIRM_URL, async ({ request }) => {
        const body = (await request.json()) as { draft_version: number };
        bodies.push(body);
        if (bodies.length === 1) {
          // Агент отредактировал черновик: версия выросла до отправки подтверждения.
          current = parseRequest({ ...f.draft, revision: 2, draft_version: 2 });
          return HttpResponse.json(
            { error: { code: 'DRAFT_CHANGED', message: 'Черновик изменён', trace_id: 'trace_mock' } },
            { status: 409 },
          );
        }
        current = parseRequest({ ...f.submitting, draft_version: 2, revision: 3 });
        return HttpResponse.json(current, { status: 202 });
      }),
    );

    const user = userEvent.setup();
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    await user.click(await screen.findByRole('button', { name: 'Подтвердить отправку' }));

    // Черновик обновлён, автоподтверждения нет — нужно явное новое нажатие.
    expect(await screen.findByText(/Параметры заявки изменились/)).toBeInTheDocument();
    const retryButton = await screen.findByRole('button', { name: 'Подтвердить отправку' });
    await user.click(retryButton);

    expect(await screen.findByText('Отправляем заявку / уточняем результат')).toBeInTheDocument();
    // Повтор отправляет уже отображённую (новую) версию черновика.
    expect(bodies).toEqual([{ draft_version: 1 }, { draft_version: 2 }]);
  });

  it('on a network failure refetches the saved state and retries with the same requestId', async () => {
    const f = createScenario();
    const bodies: unknown[] = [];
    let confirmCalls = 0;
    let current = parseRequest(f.draft);
    server.use(
      http.get(REQUEST_URL, () => HttpResponse.json(current)),
      http.post(CONFIRM_URL, async ({ request }) => {
        confirmCalls += 1;
        bodies.push(await request.json());
        if (confirmCalls === 1) return HttpResponse.error(); // исход неизвестен
        current = parseRequest(f.submitting);
        return HttpResponse.json(current, { status: 202 });
      }),
    );

    const user = userEvent.setup();
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    await user.click(await screen.findByRole('button', { name: 'Подтвердить отправку' }));

    // Локально статус не падает в failed: показан фактический черновик и заметка о сбое.
    expect(await screen.findByText(/Исход отправки неизвестен/)).toBeInTheDocument();
    expect(screen.getByText('Черновик')).toBeInTheDocument();

    // Явный повтор по тому же requestId, без новой заявки.
    await user.click(screen.getByRole('button', { name: 'Подтвердить отправку' }));
    expect(await screen.findByText('Отправляем заявку / уточняем результат')).toBeInTheDocument();
    expect(bodies).toEqual([{ draft_version: 1 }, { draft_version: 1 }]);
  });

  it('offers an explicit retry for a failed request without inventing a reason', async () => {
    const f = createScenario();
    const bodies: unknown[] = [];
    const failed = parseRequest({ ...f.draft, status: 'failed', failure_reason: 'Сервис согласования недоступен' });
    let current = failed;
    server.use(
      http.get(REQUEST_URL, () => HttpResponse.json(current)),
      http.post(CONFIRM_URL, async ({ request }) => {
        bodies.push(await request.json());
        current = parseRequest({ ...f.submitting, draft_version: failed.draft_version });
        return HttpResponse.json(current, { status: 202 });
      }),
    );

    const user = userEvent.setup();
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    expect(await screen.findByText('Сервис согласования недоступен')).toBeInTheDocument();

    await user.click(await screen.findByRole('button', { name: 'Отправить повторно' }));
    expect(await screen.findByText('Отправляем заявку / уточняем результат')).toBeInTheDocument();
    expect(bodies).toEqual([{ draft_version: 1 }]);
  });

  it('restores the saved request through GET after remount', async () => {
    const f = createScenario();
    const current = parseRequest(f.pending);
    server.use(http.get(REQUEST_URL, () => HttpResponse.json(current)));

    const first = renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    expect(await screen.findByText('Номер: ACCESS-1001')).toBeInTheDocument();
    expect(screen.getByText('Ожидает согласования')).toBeInTheDocument();
    first.unmount();

    // Reload: тот же id, состояние — только с сервера.
    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    expect(await screen.findByText('Номер: ACCESS-1001')).toBeInTheDocument();
    expect(screen.getByText('Ожидает согласования')).toBeInTheDocument();
    expect(screen.queryByText('Доступ выдан')).not.toBeInTheDocument();
  });

  it('invalidates the requests list and the plan when the status changes', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const f = createScenario();
    let current = parseRequest(f.submitting);
    let gets = 0;
    server.use(
      http.get(REQUEST_URL, () => {
        gets += 1;
        if (gets >= 2) current = parseRequest(f.granted);
        return HttpResponse.json(current);
      }),
    );

    try {
      const { queryClient } = renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
      await screen.findByText('Отправляем заявку / уточняем результат');
      const spy = vi.spyOn(queryClient, 'invalidateQueries');

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });

      await waitFor(() => expect(screen.getByText('Доступ выдан')).toBeInTheDocument());
      expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.requests() });
      expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.plan() });
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('AccessRequestPanel (#5 — polling)', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('polls submitting/pending every 5s and stops on a terminal status', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const f = createScenario();
    let current = parseRequest(f.submitting);
    let gets = 0;
    server.use(
      http.get(REQUEST_URL, () => {
        gets += 1;
        if (gets >= 2) current = parseRequest(f.granted);
        return HttpResponse.json(current);
      }),
    );

    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    await screen.findByText('Отправляем заявку / уточняем результат');
    expect(gets).toBe(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    await screen.findByText('Доступ выдан');
    expect(gets).toBe(2);

    // granted — конечный статус: следующие 5 с запросов не делают.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(gets).toBe(2);
  });

  it('skips polls while the tab is hidden and refetches on return', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const f = createScenario();
    const current = parseRequest(f.submitting);
    let gets = 0;
    server.use(
      http.get(REQUEST_URL, () => {
        gets += 1;
        return HttpResponse.json(current);
      }),
    );
    const visibilitySpy = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');

    try {
      renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
      await screen.findByText('Отправляем заявку / уточняем результат');
      expect(gets).toBe(1);

      // Вкладка скрыта: интервал тикает, запросов нет.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15000);
      });
      expect(gets).toBe(1);

      // Возвращение на вкладку: немедленная сверка без ожидания интервала.
      visibilitySpy.mockReturnValue('visible');
      await act(async () => {
        document.dispatchEvent(new Event('visibilitychange'));
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(gets).toBe(2);
    } finally {
      visibilitySpy.mockRestore();
    }
  });

  it('keeps the last known status with a refresh note when a poll fails', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const f = createScenario();
    const current = parseRequest(f.pending);
    let gets = 0;
    server.use(
      http.get(REQUEST_URL, () => {
        gets += 1;
        if (gets === 2) {
          return HttpResponse.json(
            { error: { code: 'INTERNAL', message: 'Сервис заявок недоступен', trace_id: 'trace_mock' } },
            { status: 503 },
          );
        }
        return HttpResponse.json(current);
      }),
    );

    renderWithQuery(<AccessRequestPanel requestId="request_demo_1" />);
    await screen.findByText('Ожидает согласования');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });

    // Статус не стал failed/Ошибка локально: сохранён последний успешный с заметкой.
    expect(screen.getByText('Ожидает согласования')).toBeInTheDocument();
    expect(screen.getByText('Номер: ACCESS-1001')).toBeInTheDocument();
    expect(screen.getByText(/Не удалось обновить статус/)).toBeInTheDocument();
    expect(screen.queryByText('Ошибка')).not.toBeInTheDocument();
  });
});
