import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MeetingsPage, PlanAgreements } from './MeetingsPage';
import { CuratorPage } from './CuratorPage';

vi.mock('@/features/session', () => ({ useMe: () => ({ data: { id: 'intern' } }) }));
const participants = [{ intern_id: 'intern', mentor_id: 'mentor', team_id: 'team' }];
const item = {
  id: 'meeting', intern_id: 'intern', mentor_id: 'mentor', previous_id: null,
  scheduled_at: '2030-04-15T12:00:00Z', timezone: 'UTC', status: 'scheduled', revision: 1,
  result: '', topics: [{ id: 'topic', author_id: 'intern', content: 'Личная тема', private: true }],
  agenda: [{ kind: 'checkpoint', text: 'Запустить проект', step_id: 'step', state: 'blocked', reason: 'Нет доступа' }],
  agreements: [],
};
function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'content-type': 'application/json' } });
}
afterEach(() => vi.unstubAllGlobals());

describe('Встречи 1:1', () => {
  it('планирует, переносит и завершает встречу с договорённостью', async () => {
    const fetch = vi.fn().mockImplementation((_path: string, options: RequestInit) =>
      Promise.resolve(options.method === 'POST' ? json({ revision: 2 }) : json({ participants, meetings: [item] })));
    vi.stubGlobal('fetch', fetch);
    render(<MeetingsPage />);
    await screen.findByRole('button', { name: 'Запланировать встречу' });
    const send = async (name: string, action: string) => {
      const button = screen.getByRole('button', { name });
      await waitFor(() => expect(button).toBeEnabled());
      fireEvent.submit(button.closest('form')!);
      await waitFor(() => expect(fetch.mock.calls.some(([, req]) => req.body && JSON.parse(req.body).action === action)).toBe(true));
      await waitFor(() => expect(button).toBeEnabled());
    };
    fireEvent.change(screen.getByLabelText(/^Дата и время/), { target: { value: '2030-05-01T09:00' } });
    await send('Запланировать встречу', 'create');
    await send('Перенести встречу', 'reschedule');
    fireEvent.click(screen.getByRole('button', { name: 'Новая тема' }));
    fireEvent.click(screen.getByLabelText('Видна только мне'));
    fireEvent.click(screen.getByRole('button', { name: 'Добавить договорённость' }));
    fireEvent.click(screen.getByRole('button', { name: 'Убрать договорённость' }));
    fireEvent.click(screen.getByRole('button', { name: 'Добавить договорённость' }));
    fireEvent.change(screen.getByLabelText('Итоги встречи'), { target: { value: 'Итоги' } });
    fireEvent.change(screen.getByLabelText('Текст'), { target: { value: 'Проверить доступ' } });
    fireEvent.change(screen.getByLabelText('Чекпоинт'), { target: { value: 'step' } });
    await send('Завершить встречу', 'complete');
    fireEvent.click(screen.getByRole('button', { name: 'Отменить встречу' }));
    await waitFor(() => expect(fetch.mock.calls.some(([, req]) => req.body && JSON.parse(req.body).action === 'cancel')).toBe(true));
  });
  it('показывает повестку, отправляет тему с ревизией и обновляет список', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(json({ participants, meetings: [item] }))
      .mockResolvedValueOnce(json({ revision: 2 }))
      .mockResolvedValueOnce(json({ participants, meetings: [{ ...item, revision: 2 }] }));
    vi.stubGlobal('fetch', fetch);
    render(<MeetingsPage />);
    expect(await screen.findByText('Запустить проект — Нет доступа')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Редактировать тему' }));
    fireEvent.change(screen.getByLabelText('Тема или личная заметка'), { target: { value: 'Обновлённая заметка' } });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить тему' }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
    expect(JSON.parse(fetch.mock.calls[1]?.[1].body)).toMatchObject({
      action: 'topic', revision: 1, topic_id: 'topic', private: true, content: 'Обновлённая заметка',
    });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Сохранить тему' })).toBeEnabled());
  });

  it('отмечает договорённость выполненной из плана', async () => {
    const agreement = { id: 'agreement', owner_id: 'intern', content: 'Проверить запуск', due_at: null, step_id: null, completed_at: null, revision: 1 };
    const fetch = vi.fn().mockResolvedValueOnce(json({ participants, meetings: [{ ...item, status: 'completed', agreements: [agreement] }] }))
      .mockResolvedValueOnce(json({ revision: 2 }))
      .mockResolvedValueOnce(json({ participants, meetings: [{ ...item, status: 'completed', agreements: [{ ...agreement, completed_at: '2030-04-15T13:00:00Z', revision: 2 }] }] }));
    vi.stubGlobal('fetch', fetch);
    render(<MemoryRouter><PlanAgreements /></MemoryRouter>);
    fireEvent.click(screen.getByRole('button', { name: 'Показать договорённости' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Выполнено' }));
    expect(await screen.findByText('Проверить запуск · Выполнено · Вы отвечаете')).toBeVisible();
    expect(JSON.parse(fetch.mock.calls[1]?.[1].body)).toMatchObject({ action: 'agreement-complete', revision: 1 });
  });
});

describe('Обзор куратора', () => {
  const data = {
    teams: [{ id: 'team', name: 'Команда' }], owners: [], plan_count: 2, template: null, problems: [],
    summaries: [{ kind: 'blocked', direction: 'mentor', topic: 'Доступ', events: 2, interns: 2,
      average_hours: 4, earlier_events: 1, later_events: 1, source_ids: ['step-one', 'step-two'] }],
  };
  it('создаёт проблему из сигнала и редактирует карточку', async () => {
    const stored = { id: 'id', kind: 'knowledge', direction: 'mentor', title: 'Общая проблема', description: 'Два вопроса',
      status: 'open', owner_id: null, due_at: null, resolution: '', document_id: null, template_version: null,
      merged_into: null, revision: 1, evidence: [{ kind: 'knowledge', id: 'question' }],
      history: [{ revision: 1, actor_id: 'mentor', changed_at: '2030-04-15T12:00:00Z', snapshot: { status: 'open', title: 'Общая проблема' } }] };
    const fetch = vi.fn().mockImplementation((_path: string, options: RequestInit) =>
      Promise.resolve(options.method === 'POST' ? json({ revision: 2 }) : json({ ...data, problems: [stored] })));
    vi.stubGlobal('fetch', fetch);
    render(<CuratorPage />);
    fireEvent.change(await screen.findByLabelText('Команда'), { target: { value: 'team' } });
    fireEvent.click(await screen.findByRole('button', { name: 'Создать проблему по сигналу' }));
    fireEvent.change(screen.getByLabelText('Идентификаторы исходных записей'), { target: { value: 'step-one' } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить проблему' }));
    fireEvent.change(screen.getAllByLabelText('Тип').at(-1)!, { target: { value: 'knowledge' } });
    fireEvent.change(screen.getAllByLabelText('Направление').at(-1)!, { target: { value: 'hr' } });
    fireEvent.click(screen.getByRole('button', { name: 'Редактировать проблему' }));
    fireEvent.change(screen.getByLabelText('Описание', { selector: 'textarea' }), { target: { value: 'Разобрать тему' } });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить проблему' }));
    await waitFor(() => expect(fetch.mock.calls.some(([, req]) => req.body && JSON.parse(req.body).action === 'save-problem')).toBe(true));
    fireEvent.click(screen.getByRole('button', { name: 'Добавить чекпоинт' }));
    fireEvent.click(screen.getAllByRole('button', { name: 'Убрать из шаблона' })[1]!);
  });
  it('публикует шаблон только после предпросмотра', async () => {
    const fetch = vi.fn().mockImplementation((_path: string, options: RequestInit) =>
      Promise.resolve(options.method === 'POST' ? json({ revision: 1 }) : json(data)));
    vi.stubGlobal('fetch', fetch);
    render(<CuratorPage />);
    fireEvent.change(await screen.findByLabelText('Команда'), { target: { value: 'team' } });
    expect(await screen.findByText('Блокеры · Доступ')).toBeVisible();
    fireEvent.change(screen.getByLabelText('Постоянный ключ'), { target: { value: 'setup' } });
    fireEvent.change(screen.getByLabelText('Название'), { target: { value: 'Настройка окружения' } });
    fireEvent.change(screen.getByLabelText('Описание'), { target: { value: 'Установить зависимости' } });
    fireEvent.click(screen.getByLabelText('Применить к существующим планам'));
    fireEvent.click(screen.getByRole('button', { name: 'Предпросмотр публикации' }));
    expect(fetch.mock.calls.every(([, request]) => request.method === 'GET')).toBe(true);
    expect(screen.getByText(/Будет обновлено планов: 2/)).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Опубликовать шаблон' }));
    await waitFor(() => expect(fetch.mock.calls.find(([, request]) => request.method === 'POST')).toBeDefined());
    const request = fetch.mock.calls.find(([, options]) => options.method === 'POST')?.[1];
    expect(JSON.parse(request.body)).toMatchObject({ action: 'publish-template', revision: 0, existing_plans: true, steps: [{ key: 'setup' }] });
  });
  it('убирает закрытые данные, если повторная загрузка вернула 404', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(json(data)).mockResolvedValueOnce(json(data))
      .mockResolvedValueOnce(json({ error: { code: 'NOT_FOUND', message: 'Доступ отозван', trace_id: 'trace' } }, 404));
    vi.stubGlobal('fetch', fetch);
    render(<CuratorPage />);
    fireEvent.change(await screen.findByLabelText('Команда'), { target: { value: 'team' } });
    expect(await screen.findByText('Блокеры · Доступ')).toBeVisible();
    fireEvent.change(screen.getByLabelText('Тип'), { target: { value: 'blocked' } });
    fireEvent.click(screen.getByRole('button', { name: 'Применить фильтры' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Доступ отозван');
    expect(screen.queryByText('Блокеры · Доступ')).toBeNull();
  });
});
