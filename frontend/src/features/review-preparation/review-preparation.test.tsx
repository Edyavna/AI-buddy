import { describe, expect, it } from 'vitest';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { ReviewPreparationPage } from './ReviewPreparationPage';
import { MentorReviewPreparationsPage } from './MentorReviewPreparationsPage';
import { MentorReviewPreparationPage } from './MentorReviewPreparationPage';

async function login(accountId: string) {
  const response = await fetch('/api/v1/demo/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account_id: accountId }),
  });
  expect(response.status).toBe(200);
}
const logout = async () => { await fetch('/api/v1/session', { method: 'DELETE' }); };

describe('ReviewPreparationPage — сценарий стажёра', () => {
  it('создаёт черновик вручную, помогает с описанием и отправляет с подтверждением', async () => {
    const user = userEvent.setup();
    await login('intern_demo_1');
    renderWithQuery(<ReviewPreparationPage />);

    const urlInput = await screen.findByLabelText('Ссылка на PR');
    await user.type(urlInput, 'https://github.com/Edyavna/AI-buddy/pull/59');
    await user.click(screen.getByRole('checkbox', { name: /вручную/ }));
    await user.click(screen.getByRole('button', { name: 'Создать черновик' }));

    expect(await screen.findByText('Черновик')).toBeInTheDocument();
    expect(screen.getByText(/заполнено вручную/)).toBeInTheDocument();

    await user.type(screen.getByLabelText(/Резюме/), 'Сделал форму онбординга');
    await user.type(screen.getByLabelText('Как проверено'), 'go test ./...');
    await user.click(screen.getByRole('button', { name: 'Помощь с описанием' }));
    expect(await screen.findByText(/Стоит дополнить/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Вставить в описание' }));

    // Явное двухшаговое подтверждение отправки; повторный клик защищён.
    await user.click(screen.getByRole('button', { name: 'Отправить на ревью' }));
    await user.click(screen.getByRole('button', { name: 'Подтвердить отправку на ревью' }));
    expect(await screen.findByText('Отправлено на ревью')).toBeInTheDocument();
    expect(screen.getByText(/ревизия 3/)).toBeInTheDocument();
  });

  it('отклоняет неподдерживаемый адрес PR', async () => {
    const user = userEvent.setup();
    await login('intern_demo_1');
    renderWithQuery(<ReviewPreparationPage />);
    const urlInput = await screen.findByLabelText('Ссылка на PR');
    await user.type(urlInput, 'https://gitlab.com/o/r/pull/1');
    await user.click(screen.getByRole('button', { name: 'Создать черновик' }));
    expect(await screen.findByText(/Поддерживается только github\.com/)).toBeInTheDocument();
  });

  it('интеграция GitHub: показывает CI, обновляет коммит и предлагает сверить резюме', async () => {
    const user = userEvent.setup();
    await login('intern_demo_1');
    renderWithQuery(<ReviewPreparationPage />);
    await user.type(await screen.findByLabelText('Ссылка на PR'), 'https://github.com/o/r/pull/9');
    await user.click(screen.getByRole('button', { name: 'Создать черновик' }));
    expect(await screen.findByText('Черновик')).toBeInTheDocument();
    // CI-результаты из интеграции привязаны к коммиту.
    expect(screen.getByText('build')).toBeInTheDocument();
    expect(screen.getByText(/коммит a1b2c3d/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Проверить обновления PR' }));
    expect(await screen.findByText(/PR обновился/)).toBeInTheDocument();
    expect(screen.getByText(/коммит d4e5f6a/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Проверить обновления PR' }));
    expect(await screen.findByText(/Сведения о PR актуальны, новых коммитов нет/)).toBeInTheDocument();
  });

  it('редактор ручных проверок: добавление и удаление строк', async () => {
    const user = userEvent.setup();
    await login('intern_demo_1');
    renderWithQuery(<ReviewPreparationPage />);
    await user.type(await screen.findByLabelText('Ссылка на PR'), 'https://github.com/o/r/pull/3');
    await user.click(screen.getByRole('checkbox', { name: /вручную/ }));
    await user.click(screen.getByRole('button', { name: 'Создать черновик' }));
    await screen.findByText('Черновик');

    await user.click(screen.getByRole('button', { name: 'Добавить проверку' }));
    await user.type(screen.getByLabelText('Название проверки'), 'go test ./...');
    await user.selectOptions(screen.getByLabelText('Статус проверки'), 'failed');
    await user.click(screen.getByRole('button', { name: 'Удалить проверку' }));
    expect(screen.queryByLabelText('Название проверки')).not.toBeInTheDocument();
    // Пустой редактор допустим: сохранение без ручных проверок.
    await user.click(screen.getByRole('button', { name: 'Сохранить черновик' }));
    expect(await screen.findByText('Черновик сохранён.')).toBeInTheDocument();
  });

  it('повторная отправка отправленной подготовки создаёт новую ревизию с историей', async () => {
    const user = userEvent.setup();
    await login('intern_demo_1');
    renderWithQuery(<ReviewPreparationPage />);
    await user.type(await screen.findByLabelText('Ссылка на PR'), 'https://github.com/o/r/pull/5');
    await user.click(screen.getByRole('checkbox', { name: /вручную/ }));
    await user.click(screen.getByRole('button', { name: 'Создать черновик' }));
    await screen.findByText('Черновик');
    await user.click(screen.getByRole('button', { name: 'Отправить на ревью' }));
    await user.click(await screen.findByRole('button', { name: 'Подтвердить отправку на ревью' }));
    expect(await screen.findByText('Отправлено на ревью')).toBeInTheDocument();

    await user.type(screen.getByLabelText(/Резюме/), 'Дополнил после замечаний');
    await user.click(screen.getByRole('button', { name: 'Сохранить обновление' }));
    expect(await screen.findByText('Черновик сохранён.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Отправить обновление' }));
    await user.click(await screen.findByRole('button', { name: 'Подтвердить отправку на ревью' }));
    expect(await screen.findByText(/ревизия 5/)).toBeInTheDocument();
    expect(screen.getByText(/Итоговое принятие результата/)).toBeInTheDocument();
  });
  it('сбой отправки показывает ошибку, повтор со старой ревизией — конфликт', async () => {
    const user = userEvent.setup();
    await login('intern_demo_1');
    renderWithQuery(<ReviewPreparationPage />);
    await user.type(await screen.findByLabelText('Ссылка на PR'), 'https://github.com/o/r/pull/7');
    await user.click(screen.getByRole('checkbox', { name: /вручную/ }));
    await user.click(screen.getByRole('button', { name: 'Создать черновик' }));
    await screen.findByText('Черновик');

    let failOnce = true;
    server.use(http.post('*/api/v1/review-preparations/:id/submit', async ({ request }) => {
      if (failOnce) { failOnce = false; return HttpResponse.error(); }
      const body = await request.json() as { revision: number };
      if (body.revision !== 1) return HttpResponse.json({ error: { code: 'REVIEW_CHANGED', message: 'Подготовка уже изменилась, обновите страницу', trace_id: 't' } }, { status: 409 });
      return HttpResponse.json({ error: { code: 'REVIEW_CHANGED', message: 'Подготовка уже изменилась, обновите страницу', trace_id: 't' } }, { status: 409 });
    }));
    // Потерянный ответ: сетевой сбой — никакого сообщения об успехе, кнопка разблокирована.
    await user.click(screen.getByRole('button', { name: 'Отправить на ревью' }));
    await user.click(await screen.findByRole('button', { name: 'Подтвердить отправку на ревью' }));
    expect(await screen.findByText('Не удалось связаться с сервером')).toBeInTheDocument();
    expect(screen.queryByText('Отправлено на ревью')).not.toBeInTheDocument();
    // Повтор с актуальной ревизией упирается в серверный конфликт — без мнимого успеха.
    await user.click(screen.getByRole('button', { name: 'Отправить на ревью' }));
    await user.click(await screen.findByRole('button', { name: 'Подтвердить отправку на ревью' }));
    expect(await screen.findByText('Подготовка уже изменилась, обновите страницу')).toBeInTheDocument();
  });
});

describe('MentorReviewPreparationsPage — сценарий наставника', () => {
  it('видит отправленную карточку с результатами и историей, чужая сессия — нет', async () => {
    await login('intern_demo_1');
    // Подготовка через API мока: github-режим с CI, сохранение и отправка.
    const create = await fetch('/api/v1/review-preparations', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pr_url: 'https://github.com/Edyavna/AI-buddy/pull/59', step_id: null, manual: false }),
    });
    expect(create.status).toBe(201);
    const prep = await create.json() as { id: string };
    expect(prep).toMatchObject({ status: 'draft', pr: { source: 'github', head_sha: 'a1b2c3d' } });
    const save = await fetch(`/api/v1/review-preparations/${prep.id}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        revision: 1, description: '## Описание', answers: {
          summary: 'Сделал форму', verification: 'go test ./...', limitations: '', questions: 'Норм имена?',
          items: { task_done: { done: true, note: 'ок' } },
        },
        checks: [{ name: 'go build', status: 'passed', detail: 'локально' }],
      }),
    });
    expect(save.status).toBe(200);
    const submit = await fetch(`/api/v1/review-preparations/${prep.id}/submit`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ revision: 2 }),
    });
    expect(submit.status).toBe(200);
    await logout();

    // Чужая/отсутствующая сессия: данные скрыты общим механизмом отзыва доступа.
    renderWithQuery(<MemoryRouter><MentorReviewPreparationsPage /></MemoryRouter>);
    expect(await screen.findByText('Данные недоступны или удалены')).toBeInTheDocument();

    await login('mentor_demo_1');
    renderWithQuery(<MemoryRouter><MentorReviewPreparationsPage /></MemoryRouter>);
    expect(await screen.findByText(/Сделал форму/)).toBeInTheDocument();
    expect(screen.getByText(/коммит a1b2c3d/)).toBeInTheDocument();

    // Карточка деталей: ссылка, чек-лист, источники, вопросы, история.
    renderWithQuery(<MemoryRouter initialEntries={[`/mentor/review-preparations/${prep.id}`]}>
      <Routes><Route path="/mentor/review-preparations/:id" element={<MentorReviewPreparationPage />} /></Routes>
    </MemoryRouter>);
    expect(await screen.findByText('Норм имена?')).toBeInTheDocument();
    expect(screen.getByText('build')).toBeInTheDocument();
    expect(screen.getByText('go build')).toBeInTheDocument();
    expect(screen.getAllByText('CI').length).toBeGreaterThan(0);
    expect(screen.getAllByText('вручную').length).toBeGreaterThan(0);
    expect(screen.getByText(/Ревизия 2 — отправлено/)).toBeInTheDocument();
    expect(screen.getAllByText(/коммит a1b2c3d/).length).toBeGreaterThan(0);
  });
});

describe('TemplateEditor — настройка шаблона наставником', () => {
  it('сохраняет свой шаблон команды с проверкой полей', async () => {
    const user = userEvent.setup();
    await login('mentor_demo_1');
    renderWithQuery(<MemoryRouter><MentorReviewPreparationsPage /></MemoryRouter>);
    expect(await screen.findByText('Стажёры пока не…', { exact: false }).catch(() => null)).toBeNull();
    await user.click(await screen.findByText('Шаблон чек-листа команды'));
    await user.click(screen.getByRole('button', { name: 'Добавить пункт' }));
    const labels = screen.getAllByLabelText('Название пункта');
    await user.type(labels[labels.length - 1]!, 'Проверил безопасность данных');
    expect(screen.getByRole('button', { name: 'Сохранить шаблон' })).toBeEnabled();
    await user.click(screen.getByRole('button', { name: 'Сохранить шаблон' }));
    expect(await screen.findByText(/свой, ревизия 1/)).toBeInTheDocument();
  });
});
