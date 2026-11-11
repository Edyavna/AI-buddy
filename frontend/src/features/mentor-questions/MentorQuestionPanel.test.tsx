import { QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { HttpResponse, http } from 'msw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { server } from '@/test/server';
import { renderWithQuery } from '@/test/render';
import { abortSessionRequests } from '@/shared/api/session-signal';
import type { components } from '@/shared/api/generated/schema';
import { MentorQuestionPanel } from './MentorQuestionPanel';

const messageId = 'assistant-1';
const route = '*/api/v1/messages/:id/knowledge-gap';
const question: components['schemas']['InternKnowledgeGap'] = {
  id: 'f47e17f0-47d4-401c-92f8-303cc7f676a8', message_id: messageId, status: 'new',
  revision: 1, resolution: null, document_ids: [],
  created_at: '2026-11-11T09:00:00Z', updated_at: '2026-11-11T09:00:00Z',
};

afterEach(() => vi.restoreAllMocks());

describe('передача вопроса наставнику', () => {
  it('отправляет один POST для двух карточек и восстанавливает запись после потери ответа', async () => {
    let saved = false;
    let posts = 0;
    server.use(
      http.get(route, () => HttpResponse.json({ question: saved ? question : null })),
      http.post(route, () => {
        posts++;
        saved = true;
        return HttpResponse.error();
      }),
    );
    const panel = <MentorQuestionPanel messageId={messageId} onOpenDocument={vi.fn()} onRepeatQuestion={vi.fn()} repeatDisabled={false} />;
    renderWithQuery(<>{panel}{panel}</>);
    const actions = await screen.findAllByRole('button', { name: 'Передать наставнику' });
    expect(posts).toBe(0);
    act(() => {
      fireEvent.click(actions[0]!);
      fireEvent.click(actions[1]!);
    });
    await waitFor(() => expect(screen.getAllByText('Передан наставнику')).toHaveLength(2));
    expect(posts).toBe(1);
  });

  it('восстанавливает решение, показывает новую ревизию и скрывает данные при отзыве доступа', async () => {
    let denied = false;
    let current: typeof question = { ...question, status: 'resolved', revision: 3, resolution: 'Смотрите инструкцию', document_ids: ['doc'] };
    server.use(http.get(route, () => denied
      ? HttpResponse.json({ error: { code: 'NOT_FOUND', message: 'Не найдено', trace_id: 'test' } }, { status: 404 })
      : HttpResponse.json({ question: current })));
    const open = vi.fn(), repeat = vi.fn();
    const panel = (disabled: boolean) => <MentorQuestionPanel messageId={messageId} onOpenDocument={open} onRepeatQuestion={repeat} repeatDisabled={disabled} />;
    const { queryClient, rerender } = renderWithQuery(panel(true));
    fireEvent.click(await screen.findByRole('button', { name: 'Открыть документ' }));
    expect(open).toHaveBeenCalledWith('doc');
    expect(screen.getByRole('button', { name: 'Повторить вопрос' })).toBeDisabled();
    rerender(<QueryClientProvider client={queryClient}>{panel(false)}</QueryClientProvider>);
    fireEvent.click(screen.getByRole('button', { name: 'Повторить вопрос' }));
    expect(repeat).toHaveBeenCalledOnce();

    current = question;
    await act(() => queryClient.refetchQueries());
    expect(screen.getByText('Смотрите инструкцию')).toBeInTheDocument();
    current = { ...question, status: 'in_progress', revision: 4 };
    await act(() => queryClient.refetchQueries());
    expect(await screen.findByText('В работе')).toBeInTheDocument();
    expect(screen.queryByText('Смотрите инструкцию')).toBeNull();
    denied = true;
    await act(() => queryClient.refetchQueries());
    expect(await screen.findByText('Вопрос недоступен')).toBeInTheDocument();
    expect(screen.queryByText('В работе')).toBeNull();
  });

  it('отменяет передачу при сбросе сессии', async () => {
    let postStarted = false;
    let aborted = false;
    server.use(
      http.get(route, () => HttpResponse.json({ question: null })),
      http.post(route, async ({ request }) => {
        postStarted = true;
        await new Promise<void>(resolve => request.signal.addEventListener('abort', () => { aborted = true; resolve(); }, { once: true }));
        return HttpResponse.error();
      }),
    );
    const { queryClient } = renderWithQuery(<MentorQuestionPanel messageId={messageId} onOpenDocument={vi.fn()} onRepeatQuestion={vi.fn()} repeatDisabled={false} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Передать наставнику' }));
    await waitFor(() => expect(postStarted).toBe(true));
    abortSessionRequests(queryClient);
    await waitFor(() => expect(aborted).toBe(true));
  });
});
