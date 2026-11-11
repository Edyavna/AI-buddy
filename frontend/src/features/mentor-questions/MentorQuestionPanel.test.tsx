import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { delay, http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createQueryClient, queryKeys } from '@/shared/api';
import { resetSessionState } from '@/features/session/session-state';
import { MentorQuestionPanel } from './MentorQuestionPanel';
import { canEscalateMessage } from './can-escalate';
import { acceptMentorQuestion } from './cache';
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
const resolvedQuestion: MentorQuestion = {
  ...baseQuestion,
  status: 'resolved',
  revision: 3,
  resolution: 'Обновлённая инструкция находится в приложенном документе.',
  document_ids: ['doc_local_setup'],
  updated_at: '2026-11-09T12:10:00Z',
};

function jsonError(status: number, message = 'Действие недоступно') {
  return HttpResponse.json({ error: { code: 'ERROR', message, trace_id: 'trace_mock' } }, { status });
}

function Panel({ busy = false }: { busy?: boolean }) {
  return <MentorQuestionPanel
    messageId={MESSAGE_ID}
    canEscalate
    isChatBusy={busy}
    onOpenDocument={() => {}}
    onRepeatQuestion={() => {}}
  />;
}

describe('canEscalateMessage', () => {
  it('allows only a completed assistant answer with knowledge_gap', () => {
    expect(canEscalateMessage({ role: 'assistant', status: 'completed', outcome: 'knowledge_gap' })).toBe(true);
    for (const outcome of ['answered', 'needs_clarification', 'unknown', undefined] as const) {
      expect(canEscalateMessage({ role: 'assistant', status: 'completed', outcome })).toBe(false);
    }
    expect(canEscalateMessage({ role: 'assistant', status: 'failed', outcome: 'knowledge_gap' })).toBe(false);
    expect(canEscalateMessage({ role: 'assistant', status: 'interrupted', outcome: 'knowledge_gap' })).toBe(false);
    expect(canEscalateMessage({ role: 'assistant', status: 'generating' })).toBe(false);
    expect(canEscalateMessage({ role: 'user', status: 'completed', outcome: 'knowledge_gap' })).toBe(false);
  });
});

describe('MentorQuestionPanel — escalation', () => {
  it('does not render or request an escalation for a regular answer', async () => {
    let requests = 0;
    server.use(
      http.get(URL, () => { requests += 1; return HttpResponse.json({ question: null }); }),
      http.post(URL, () => { requests += 1; return HttpResponse.json(baseQuestion, { status: 201 }); }),
    );
    const { container } = renderWithQuery(<MentorQuestionPanel
      messageId={MESSAGE_ID}
      canEscalate={false}
      onOpenDocument={() => {}}
      onRepeatQuestion={() => {}}
    />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByRole('button', { name: 'Передать наставнику' })).not.toBeInTheDocument();
    await waitFor(() => expect(requests).toBe(0));
  });

  it('sends POST only after an explicit click and shows the transferred state', async () => {
    let posts = 0;
    server.use(http.post(URL, () => {
      posts += 1;
      return HttpResponse.json(baseQuestion, { status: 201 });
    }));
    const user = userEvent.setup();
    renderWithQuery(<Panel />);
    const button = await screen.findByRole('button', { name: 'Передать наставнику' });
    expect(posts).toBe(0);
    await user.click(button);
    expect(await screen.findByText('Передан наставнику')).toBeInTheDocument();
    expect(posts).toBe(1);
    expect(screen.queryByRole('button', { name: 'Передать наставнику' })).not.toBeInTheDocument();
  });

  it('deduplicates a double click and two mounted panels', async () => {
    let posts = 0;
    let release!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; });
    server.use(http.post(URL, async () => {
      posts += 1;
      await gate;
      return HttpResponse.json(baseQuestion, { status: 201 });
    }));
    renderWithQuery(<><Panel /><Panel /></>);
    const buttons = await screen.findAllByRole('button', { name: 'Передать наставнику' });
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[0]!);
    fireEvent.click(buttons[0]!);
    fireEvent.click(buttons[1]!);
    await waitFor(() => expect(posts).toBe(1));
    release();
    await waitFor(() => expect(screen.getAllByText('Передан наставнику')).toHaveLength(2));
    expect(posts).toBe(1);
  });

  it('restores the existing record through GET when the POST response is lost', async () => {
    let posts = 0;
    let gets = 0;
    let created: MentorQuestion | null = null;
    server.use(
      http.get(URL, () => {
        gets += 1;
        return HttpResponse.json({ question: created });
      }),
      http.post(URL, () => {
        posts += 1;
        created = baseQuestion;
        return HttpResponse.error();
      }),
    );
    const user = userEvent.setup();
    renderWithQuery(<Panel />);
    await user.click(await screen.findByRole('button', { name: 'Передать наставнику' }));
    expect(await screen.findByText('Передан наставнику')).toBeInTheDocument();
    expect(posts).toBe(1);
    expect(gets).toBe(2);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps a retryable error when neither POST nor reconciliation confirms creation', async () => {
    server.use(
      http.get(URL, () => HttpResponse.json({ question: null })),
      http.post(URL, () => HttpResponse.error()),
    );
    const user = userEvent.setup();
    renderWithQuery(<Panel />);
    await user.click(await screen.findByRole('button', { name: 'Передать наставнику' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось подтвердить передачу');
    expect(screen.getByRole('button', { name: 'Передать наставнику' })).toBeEnabled();
    expect(screen.queryByText('Передан наставнику')).not.toBeInTheDocument();
  });

  it('shows a 503 without turning it into a successful escalation', async () => {
    server.use(http.post(URL, () => jsonError(503, 'Очередь временно недоступна')));
    const user = userEvent.setup();
    renderWithQuery(<Panel />);
    await user.click(await screen.findByRole('button', { name: 'Передать наставнику' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Очередь временно недоступна');
    expect(screen.queryByText('Передан наставнику')).not.toBeInTheDocument();
  });
});

describe('MentorQuestionPanel — resolution and access', () => {
  it('opens a document and repeats the question once through callbacks', async () => {
    const openDocument = vi.fn();
    const repeat = vi.fn();
    server.use(http.get(URL, () => HttpResponse.json({ question: resolvedQuestion })));
    const user = userEvent.setup();
    renderWithQuery(<MentorQuestionPanel
      messageId={MESSAGE_ID}
      canEscalate
      onOpenDocument={openDocument}
      onRepeatQuestion={repeat}
    />);
    expect(await screen.findByText('Решён')).toBeInTheDocument();
    expect(screen.getByText(resolvedQuestion.resolution!)).toBeInTheDocument();

    await user.tab();
    expect(screen.getByRole('button', { name: 'Открыть источник 1' })).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(openDocument).toHaveBeenCalledExactlyOnceWith('doc_local_setup');
    await user.tab();
    expect(screen.getByRole('button', { name: 'Повторить вопрос' })).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(repeat).toHaveBeenCalledOnce();
  });

  it('disables repeat while another answer is generating', async () => {
    server.use(http.get(URL, () => HttpResponse.json({ question: resolvedQuestion })));
    renderWithQuery(<Panel busy />);
    expect(await screen.findByRole('button', { name: 'Повторить вопрос' })).toBeDisabled();
    expect(screen.getByText('Дождитесь завершения текущего ответа.')).toBeInTheDocument();
  });

  it.each([401, 403, 404])('hides cached resolution and actions after access error %s', async status => {
    let allowed = true;
    server.use(http.get(URL, () => allowed
      ? HttpResponse.json({ question: resolvedQuestion })
      : jsonError(status)));
    function Harness() {
      const [visible, setVisible] = useState(true);
      return <><button onClick={() => setVisible(value => !value)}>Переключить панель</button>
        {visible && <Panel />}</>;
    }
    const { queryClient } = renderWithQuery(<Harness />);
    expect(await screen.findByText(resolvedQuestion.resolution!)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Переключить панель' }));
    allowed = false;
    fireEvent.click(screen.getByRole('button', { name: 'Переключить панель' }));
    await waitFor(() => expect(queryClient.getQueryState(queryKeys.mentorQuestion(MESSAGE_ID))?.status).toBe('error'));
    expect(screen.queryByText(resolvedQuestion.resolution!)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Повторить вопрос' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Открыть источник 1' })).not.toBeInTheDocument();
    expect(screen.getByText('Вопрос наставнику недоступен')).toBeInTheDocument();
  });

  it('keeps the last known resolution on a temporary refresh failure', async () => {
    let available = true;
    server.use(http.get(URL, () => available
      ? HttpResponse.json({ question: resolvedQuestion })
      : jsonError(503, 'Повторите позже')));
    const { queryClient } = renderWithQuery(<Panel />);
    expect(await screen.findByText(resolvedQuestion.resolution!)).toBeInTheDocument();
    available = false;
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    document.dispatchEvent(new Event('visibilitychange'));
    await waitFor(() => expect(queryClient.getQueryState(queryKeys.mentorQuestion(MESSAGE_ID))?.status).toBe('error'));
    expect(screen.getByText(resolvedQuestion.resolution!)).toBeInTheDocument();
    expect(screen.getByText(/Показано последнее известное состояние/)).toBeInTheDocument();
    visibility.mockRestore();
  });

  it('offers retry after an initial loading error', async () => {
    let available = false;
    server.use(http.get(URL, () => available
      ? HttpResponse.json({ question: null })
      : jsonError(503, 'Сервис вопросов недоступен')));
    const user = userEvent.setup();
    renderWithQuery(<Panel />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Сервис вопросов недоступен');
    available = true;
    await user.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(await screen.findByRole('button', { name: 'Передать наставнику' })).toBeInTheDocument();
  });

  it('clears cached details and aborts an active operation on session reset', async () => {
    let posts = 0;
    server.use(http.post(URL, async () => {
      posts += 1;
      await delay(100);
      return HttpResponse.json(baseQuestion, { status: 201 });
    }));
    const view = renderWithQuery(<Panel />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Передать наставнику' }));
    await waitFor(() => expect(posts).toBe(1));
    view.unmount();
    view.queryClient.setQueryData(queryKeys.mentorQuestion(MESSAGE_ID), { question: resolvedQuestion });
    await resetSessionState(view.queryClient);
    expect(view.queryClient.getQueryData(queryKeys.mentorQuestion(MESSAGE_ID))).toBeUndefined();
    expect(screen.queryByText(resolvedQuestion.resolution!)).not.toBeInTheDocument();
    await delay(150);
    expect(view.queryClient.getQueryData(queryKeys.mentorQuestion(MESSAGE_ID))).toBeUndefined();
  });

  it('drops an older revision and a contradictory message id at the cache boundary', () => {
    const client = createQueryClient();
    acceptMentorQuestion(client, MESSAGE_ID, resolvedQuestion);
    expect(acceptMentorQuestion(client, MESSAGE_ID, baseQuestion).question).toEqual(resolvedQuestion);
    expect(acceptMentorQuestion(client, MESSAGE_ID, null).question).toEqual(resolvedQuestion);
    expect(() => acceptMentorQuestion(client, MESSAGE_ID, { ...baseQuestion, message_id: 'other' })).toThrow('другого сообщения');
    client.clear();
  });
});
