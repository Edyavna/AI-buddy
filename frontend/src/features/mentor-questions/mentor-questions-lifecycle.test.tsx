import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, screen, waitFor } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { MentorQuestionPanel } from './MentorQuestionPanel';
import type { MentorQuestion } from './schemas';

const MESSAGE_ID = 'message_gap';
const URL = '*/api/v1/messages/:id/knowledge-gap';
const baseQuestion: MentorQuestion = {
  id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  message_id: MESSAGE_ID,
  status: 'new',
  revision: 1,
  resolution: null,
  document_ids: [],
  created_at: '2026-11-09T12:00:00Z',
  updated_at: '2026-11-09T12:00:00Z',
};
const inProgress: MentorQuestion = {
  ...baseQuestion,
  status: 'in_progress',
  revision: 2,
  updated_at: '2026-11-09T12:05:00Z',
};
const resolved: MentorQuestion = {
  ...baseQuestion,
  status: 'resolved',
  revision: 3,
  resolution: 'Решение сохранено на сервере.',
  document_ids: [],
  updated_at: '2026-11-09T12:10:00Z',
};

function Panel() {
  return <MentorQuestionPanel
    messageId={MESSAGE_ID}
    canEscalate
    onOpenDocument={() => {}}
    onRepeatQuestion={() => {}}
  />;
}

describe('MentorQuestionPanel — lifecycle', () => {
  it.each([401, 403, 404])('keeps revoked details hidden after %s and a failed refresh, then recovers', async (deniedStatus) => {
    let status = 200;
    const question = { ...resolved, document_ids: ['private_document'] };
    server.use(http.get(URL, () => status === 200
      ? HttpResponse.json({ question })
      : HttpResponse.json({}, { status })));
    const { queryClient } = renderWithQuery(<Panel />);
    const key = ['mentor-questions', 'message', MESSAGE_ID];
    await screen.findByText(resolved.resolution!);
    status = deniedStatus;
    await act(async () => { await queryClient.refetchQueries({ queryKey: key }); });
    await screen.findByText('Вопрос наставнику недоступен');
    expect(queryClient.getQueryData(key)).toEqual({ question: null, accessDenied: true });
    status = 503;
    await act(async () => { await queryClient.refetchQueries({ queryKey: key }); });
    expect(screen.getByText('Вопрос наставнику недоступен')).toBeInTheDocument();
    expect(screen.queryByText(resolved.resolution!)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Открыть источник 1' })).not.toBeInTheDocument();
    status = 200;
    document.dispatchEvent(new Event('visibilitychange'));
    await screen.findByText(resolved.resolution!);
    expect(screen.getByRole('button', { name: 'Открыть источник 1' })).toBeInTheDocument();
    expect(screen.queryByText('Вопрос наставнику недоступен')).not.toBeInTheDocument();
  });

  it('does not refetch cached data on tab return after escalation is disabled', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let gets = 0;
    server.use(http.get(URL, () => {
      gets += 1;
      return HttpResponse.json({ question: resolved });
    }));
    const view = renderWithQuery(<Panel />);
    await screen.findByText(resolved.resolution!);
    view.rerender(<QueryClientProvider client={view.queryClient}>
      <MentorQuestionPanel messageId={MESSAGE_ID} canEscalate={false}
        onOpenDocument={() => {}} onRepeatQuestion={() => {}} />
    </QueryClientProvider>);
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(gets).toBe(1);
    expect(view.container).toBeEmptyDOMElement();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('pauses polling while the panel is outside the active screen', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let observerCallback: IntersectionObserverCallback | undefined;
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: IntersectionObserverCallback) { observerCallback = callback; }
      observe() {}
      disconnect() {}
    });
    let gets = 0;
    server.use(http.get(URL, () => {
      gets += 1;
      return HttpResponse.json({ question: gets === 1 ? baseQuestion : inProgress });
    }));
    renderWithQuery(<Panel />);
    expect(await screen.findByText('Передан наставнику')).toBeInTheDocument();
    expect(observerCallback).toBeDefined();

    await act(async () => {
      observerCallback?.([{ isIntersecting: false } as IntersectionObserverEntry], {} as IntersectionObserver);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000);
    });
    expect(gets).toBe(1);
    document.dispatchEvent(new Event('visibilitychange'));
    expect(gets).toBe(1);

    await act(async () => {
      observerCallback?.([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver);
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(await screen.findByText('В работе')).toBeInTheDocument();
    expect(gets).toBe(2);
  });

  it('polls new/in_progress every 5 seconds and stops at resolved', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let gets = 0;
    server.use(http.get(URL, () => {
      gets += 1;
      const question = gets === 1 ? baseQuestion : gets === 2 ? inProgress : resolved;
      return HttpResponse.json({ question });
    }));
    renderWithQuery(<Panel />);
    expect(await screen.findByText('Передан наставнику')).toBeInTheDocument();
    expect(gets).toBe(1);

    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(await screen.findByText('В работе')).toBeInTheDocument();
    expect(gets).toBe(2);

    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(await screen.findByText('Решён')).toBeInTheDocument();
    expect(screen.getByText(resolved.resolution!)).toBeInTheDocument();
    expect(gets).toBe(3);

    await act(async () => { await vi.advanceTimersByTimeAsync(15000); });
    expect(gets).toBe(3);
  });

  it('does not poll a hidden tab and reconciles immediately on return', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let gets = 0;
    server.use(http.get(URL, () => {
      gets += 1;
      return HttpResponse.json({ question: gets === 1 ? baseQuestion : inProgress });
    }));
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    try {
      renderWithQuery(<Panel />);
      expect(await screen.findByText('Передан наставнику')).toBeInTheDocument();
      expect(gets).toBe(1);
      await act(async () => { await vi.advanceTimersByTimeAsync(15000); });
      expect(gets).toBe(1);

      visibility.mockReturnValue('visible');
      await act(async () => {
        document.dispatchEvent(new Event('visibilitychange'));
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(await screen.findByText('В работе')).toBeInTheDocument();
      expect(gets).toBe(2);
    } finally {
      visibility.mockRestore();
    }
  });

  it('shows a reopened question when returning to a resolved panel', async () => {
    let reopened = false;
    server.use(http.get(URL, () => HttpResponse.json({
      question: reopened ? { ...inProgress, revision: 4 } : resolved,
    })));
    renderWithQuery(<Panel />);
    expect(await screen.findByText('Решён')).toBeInTheDocument();
    reopened = true;
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    try {
      document.dispatchEvent(new Event('visibilitychange'));
      expect(await screen.findByText('В работе')).toBeInTheDocument();
      expect(screen.queryByText(resolved.resolution!)).not.toBeInTheDocument();
    } finally {
      visibility.mockRestore();
    }
  });

  it('does not roll back a resolved record after an older GET response', async () => {
    let stale = false;
    server.use(http.get(URL, () => HttpResponse.json({ question: stale ? baseQuestion : resolved })));
    renderWithQuery(<Panel />);
    expect(await screen.findByText('Решён')).toBeInTheDocument();
    stale = true;
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    try {
      document.dispatchEvent(new Event('visibilitychange'));
      await waitFor(() => expect(screen.getByText('Решён')).toBeInTheDocument());
      expect(screen.getByText(resolved.resolution!)).toBeInTheDocument();
      expect(screen.queryByText('Передан наставнику')).not.toBeInTheDocument();
    } finally {
      visibility.mockRestore();
    }
  });

  it('restores state from API after remount with a cleared query cache', async () => {
    let gets = 0;
    server.use(http.get(URL, () => {
      gets += 1;
      return HttpResponse.json({ question: resolved });
    }));
    const first = renderWithQuery(<Panel />);
    expect(await screen.findByText(resolved.resolution!)).toBeInTheDocument();
    first.unmount();
    first.queryClient.clear();

    const second = renderWithQuery(<Panel />);
    expect(await screen.findByText(resolved.resolution!)).toBeInTheDocument();
    expect(gets).toBe(2);
    expect(second.queryClient.getQueryData(['mentor-questions', 'message', MESSAGE_ID])).toMatchObject({
      question: { revision: 3, status: 'resolved' },
    });
  });
});
