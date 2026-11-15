import { webcrypto } from 'node:crypto';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MeetingSuggestion } from './index';
import { MeetingsPage } from './MeetingsPage';

const session = vi.hoisted(() => ({ user: { id: 'intern', role: 'intern' } }));
vi.mock('@/features/session', () => ({ useMe: () => ({ data: session.user }) }));

const draft = {
  id: 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', reason: 'explicit', mentor_id: 'mentor', existing_id: '',
  date: '2030-05-01', time: '15:00', timezone: 'Europe/Moscow', title: 'Цели на месяц',
  topics: ['Первая задача', 'Критерии успеха'], missing: [],
};
const question = { id: 'question', kind: 'question', text: 'Получение ноутбука', step_id: null, state: 'open', included: true };
const participants = [{ intern_id: 'intern', mentor_id: 'mentor', team_id: 'team' }];
const item = {
  id: draft.id, intern_id: 'intern', mentor_id: 'mentor', previous_id: null,
  scheduled_at: '2030-05-01T12:00:00Z', timezone: 'Europe/Moscow', status: 'pending',
  revision: 1, title: draft.title, call_url: 'https://example.com/call', result: '',
  agenda: [question], topics: [{ id: 'topic', author_id: 'mentor', content: 'Общая тема', private: false }],
  agreements: [],
};
function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'content-type': 'application/json' } });
}
function setup(path = '/chat', state?: unknown) {
  const client = new QueryClient();
  return render(<QueryClientProvider client={client}>
    <MemoryRouter initialEntries={[{ pathname: path, state }]}>
      <Routes>
        <Route path="/chat" element={<MeetingSuggestion messageID="reply" owner="intern" />} />
        <Route path="/meetings" element={<><Link to="/chat">Чат</Link><MeetingsPage /></>} />
      </Routes>
    </MemoryRouter>
  </QueryClientProvider>);
}
afterEach(() => {
  session.user = { id: 'intern', role: 'intern' };
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

describe('Приглашение на встречу', () => {
  it('открывает черновик из чата, редактирует темы и явно отправляет одно приглашение', async () => {
    let sent = false;
    const submit = vi.fn();
    vi.stubGlobal('fetch', vi.fn(async (path: string, request: RequestInit) => {
      if (path.includes('/suggestion')) return json({ proposal: draft });
      if (request.method === 'POST') { submit(JSON.parse(String(request.body))); sent = true; return json({ revision: 1 }); }
      return json({ participants, candidates: [question], meetings: sent ? [item] : [] });
    }));
    setup();
    fireEvent.click(await screen.findByRole('link', { name: 'Открыть черновик встречи' }));
    expect(await screen.findByLabelText('Тема встречи')).toHaveValue(draft.title);
    expect(screen.getByLabelText('Дата')).toHaveValue(draft.date);
    expect(screen.getByLabelText('Время')).toHaveValue(draft.time);
    expect(screen.getByLabelText('Часовой пояс')).toHaveValue(draft.timezone);
    expect(screen.getByLabelText('Наставник')).toHaveValue(draft.mentor_id);
    expect(screen.getByLabelText(/Свои темы/)).toHaveValue(draft.topics.join('\n'));
    expect(submit).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText(/Свои темы/), { target: { value: 'Моя тема\n \n Критерии' } });
    fireEvent.click(screen.getByRole('checkbox', { name: /Получение ноутбука/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Отправить приглашение' }));
    expect(await screen.findByText(/Ожидает ответа наставника/)).toBeVisible();
    expect(submit).toHaveBeenCalledExactlyOnceWith({
      id: draft.id, action: 'invite', mentor_id: 'mentor', title: draft.title,
      local_at: '2030-05-01T15:00', timezone: draft.timezone, call_url: '',
      topics: ['Моя тема', 'Критерии'],
      agenda_choices: [{ kind: 'question', id: question.id, included: true }],
    });
    expect(screen.queryByRole('button', { name: 'Принять приглашение' })).toBeNull();
  });

  it('подсказывает уточнения и предлагает ручную форму при сбое модели', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(json({ proposal: { ...draft, missing: ['участник', 'время'] } }))
      .mockResolvedValueOnce(json({}, 503)));
    const view = setup();
    expect(await screen.findByText(/участник, время/)).toBeVisible();
    view.unmount();
    setup();
    expect(await screen.findByRole('link', { name: 'Подготовить встречу вручную' })).toHaveAttribute('href', '/meetings');
  });

  it.each([200, 404])('скрывает предложение при ответе %d без доступной встречи', async status => {
    const fetch = vi.fn().mockResolvedValue(json({ proposal: null }, status));
    vi.stubGlobal('fetch', fetch);
    setup();
    await waitFor(() => expect(fetch).toHaveBeenCalledOnce());
    expect(screen.queryByLabelText('Предложение встречи')).toBeNull();
  });

  it('добавляет темы в существующую встречу и сохраняет исключение вопроса', async () => {
    let meeting = { ...item };
    const save = vi.fn();
    vi.stubGlobal('fetch', vi.fn(async (path: string, request: RequestInit) => {
      if (path.includes('/suggestion')) return json({ proposal: { ...draft, title: '', existing_id: item.id } });
      if (request.method === 'POST') {
        const body = JSON.parse(String(request.body));
        save(body);
        if (body.action === 'agenda') meeting = { ...meeting, agenda: [{ ...question, included: false }], revision: 2 };
        return json({ revision: meeting.revision });
      }
      return json({ participants, meetings: [meeting] });
    }));
    setup();
    fireEvent.click(await screen.findByRole('link', { name: 'Добавить темы в запланированную встречу' }));
    const topics = await screen.findByLabelText(/Темы из чата/);
    expect(topics).toHaveValue(draft.topics.join('\n'));
    fireEvent.click(screen.getByRole('button', { name: 'Добавить в повестку' }));
    await waitFor(() => expect(topics).toHaveValue(''));
    expect(save).toHaveBeenLastCalledWith({ id: item.id, revision: 1, action: 'topics-add', topics: draft.topics });
    fireEvent.click(screen.getByRole('checkbox', { name: /Получение ноутбука/ }));
    await waitFor(() => expect(screen.getByRole('checkbox', { name: /Получение ноутбука/ })).not.toBeChecked());
    expect(save).toHaveBeenLastCalledWith({
      id: item.id, revision: 1, action: 'agenda', agenda_choices: [{ kind: 'question', id: question.id, included: false }],
    });
    const card = screen.getByRole('article');
    expect(within(card).getByText('Выбранных рабочих вопросов нет.')).toBeVisible();
    fireEvent.focus(window);
    expect(screen.getByRole('checkbox', { name: /Получение ноутбука/ })).not.toBeChecked();
  });

  it.each([['accept', 'Принять', 'accepted', 'Принята'], ['decline', 'Отклонить', 'declined', 'Отклонена']] as const)(
    'показывает обеим сторонам решение %s', async (action, label, status, text) => {
      session.user = { id: 'mentor', role: 'mentor' };
      let current = { ...item };
      const save = vi.fn();
      vi.stubGlobal('fetch', vi.fn(async (_path: string, request: RequestInit) => {
        if (request.method === 'POST') {
          save(JSON.parse(String(request.body)));
          current = { ...current, revision: 2, status };
          return json({ revision: 2 });
        }
        return json({ participants, meetings: [current] });
      }));
      const view = setup('/meetings');
      fireEvent.click(await screen.findByRole('button', { name: `${label} приглашение` }));
      expect(await screen.findByText(new RegExp(`^${text}`))).toBeVisible();
      expect(save).toHaveBeenCalledExactlyOnceWith({ id: item.id, action, revision: 1 });
      view.unmount();
      session.user = { id: 'intern', role: 'intern' };
      setup('/meetings');
      expect(await screen.findByText(new RegExp(`^${text}`))).toBeVisible();
      expect(screen.getByRole('link', { name: 'Перейти к звонку' })).toHaveAttribute('href', item.call_url);
      const calendar = screen.queryByRole('link', { name: 'Скачать .ics' });
      if (status === 'accepted') expect(calendar).toHaveAttribute('href', `/api/v1/meetings/${item.id}/calendar`);
      else expect(calendar).toBeNull();
    },
  );

  it('повторяет ручную отправку после сетевой ошибки с прежним идентификатором', async () => {
    vi.stubGlobal('crypto', webcrypto);
    const save = vi.fn();
    vi.stubGlobal('fetch', vi.fn(async (_path: string, request: RequestInit) => {
      if (request.method !== 'POST') return json({ participants, meetings: [] });
      save(JSON.parse(String(request.body)));
      if (save.mock.calls.length === 1) throw new Error('network');
      return json({ revision: 1 });
    }));
    setup('/meetings');
    const button = await screen.findByRole('button', { name: 'Отправить приглашение' });
    fireEvent.change(screen.getByLabelText('Тема встречи'), { target: { value: 'Согласовать план' } });
    fireEvent.change(screen.getByLabelText('Дата'), { target: { value: draft.date } });
    fireEvent.change(screen.getByLabelText('Время'), { target: { value: draft.time } });
    fireEvent.click(button);
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось связаться с сервером');
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
    expect(save.mock.calls[0]?.[0]).toEqual(save.mock.calls[1]?.[0]);
  });
});
