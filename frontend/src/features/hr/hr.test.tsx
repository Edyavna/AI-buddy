import { afterEach, beforeEach, expect, it } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { App, queryClient } from '@/app/App';
import { routes } from '@/app/router';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { QuestionComposer } from './QuestionComposer';
import { QueryClientProvider } from '@tanstack/react-query';
import type {ContextInput,ContextSnapshot} from '@/features/question-context/api';

const team = { id: 'backend', name: 'Backend' };
const date = '2026-11-13T09:00:00Z';
const employee = { id: 'hr1', role: 'hr', name: 'Анна HR', team };
const item = {
  context_snapshot:undefined as ContextSnapshot|undefined,
  id: 'q1', message_id: 'm1', team_id: team.id, direction: 'hr', original_direction: 'hr',
  student: { id: 'intern', name: 'Стажёр' }, specialist_id: null as string | null,
  step_id: null, status: 'new', revision: 1, question: 'Как оформить отпуск?', context_excerpt: 'Контекст',
  resolution: null as string | null, document_ids: [] as string[], routing: null,
  created_at: date, updated_at: date, applied_revision: null,
};
const document = {
  id: 'doc1', author_id: employee.id, direction: 'hr', title: 'Инструкция по отпуску', content: '# Отпуск',
  team_ids: [team.id], version: 1, published_version: null as number | null,
  revoked: false, status: 'draft', error: null as string | null, updated_at: date,
};
const missing = (status = 404) => HttpResponse.json({ error: { code: 'NOT_FOUND', message: 'Недоступно', trace_id: 'hr-test' } }, { status });
const approvedContext=(input:ContextInput|undefined):ContextSnapshot|undefined=>input?{conversation_id:'mock-conversation',attempts:input.attempts,step:null,fragments:[],documents:[],limited:false}:undefined;

beforeEach(() => queryClient.clear());
afterEach(() => queryClient.clear());

function mount(path: string, user = employee, access = () => ({ version: "1", teams: [team] })) {
  server.use(http.get('*/api/v1/me', () => HttpResponse.json(user)),
    http.get('*/api/v1/me/access-scope', () => HttpResponse.json(access())));
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(<App routerOverride={router} />);
  return router;
}

it('opens the HR queue from a protected intern route and completes a claimed question', async () => {
  let state = { ...item };
  server.use(
    http.get('*/api/v1/hr/questions', ({ request }) => {
      const filter = new URL(request.url).searchParams.get('status');
      return HttpResponse.json({ items: filter === 'resolved' ? [] : [state] });
    }),
    http.get('*/api/v1/hr/questions/q1', () => HttpResponse.json(state)),
    http.get('*/api/v1/hr/documents', () => HttpResponse.json({ items: [{ ...document, status: 'ready', published_version: 1 }] })),
    http.patch('*/api/v1/hr/questions/q1', async ({ request }) => {
      const body = await request.json() as { action: string; resolution?: string; document_ids?: string[] };
      state = { ...state, specialist_id: employee.id, status: body.action === 'resolve' ? 'resolved' : 'in_progress',
        revision: state.revision + 1, resolution: body.resolution ?? state.resolution, document_ids: body.document_ids ?? state.document_ids };
      // The claim commits even though the response is lost.
      return body.action === 'claim' ? HttpResponse.error() : HttpResponse.json(state);
    }),
    http.get('*/api/v1/documents/doc1', () => HttpResponse.json(document)),
  );
  const router = mount('/onboarding');
  expect(await screen.findByRole('heading', { name: 'Кадровые обращения' })).toBeVisible();
  expect(router.state.location.pathname).toBe('/hr/questions');
  fireEvent.change(screen.getByLabelText('Статус'), { target: { value: 'resolved' } });
  expect(await screen.findByText('Обращений пока нет.')).toBeVisible();
  fireEvent.change(screen.getByLabelText('Статус'), { target: { value: '' } });
  fireEvent.change(screen.getByLabelText('Команда'), { target: { value: team.id } });
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'отпуск' } });
  fireEvent.click(await screen.findByRole('link', { name: item.question }));
  fireEvent.click(await screen.findByRole('button', { name: 'Взять в работу' }));
  fireEvent.change(await screen.findByLabelText('Ответ стажёру'), { target: { value: 'Подайте заявление' } });
  fireEvent.click(await screen.findByRole('checkbox', { name: document.title }));
  fireEvent.click(screen.getByRole('button', { name: 'Отправить ответ' }));
  expect(await screen.findByText('Подайте заявление')).toBeVisible();
  fireEvent.click(await screen.findByRole('button', { name: 'Открыть инструкцию' }));
  expect(await screen.findByRole('dialog', { name: document.title })).toBeVisible();
  fireEvent.keyDown(screen.getByRole('dialog', { name: document.title }), { key: 'Escape' });
  fireEvent.click(screen.getByRole('button', { name: 'Решить обращение' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Вернуть в работу' }));
  expect(await screen.findByRole('button', { name: 'Отправить ответ' })).toBeVisible();
});

it('keeps an HR draft without a recipient, sends after access returns, and reroutes with a preview', async () => {
  let allowed = false;
  let questions: (typeof item)[] = [];
  let drafts: { direction: string; question: string; context_excerpt: string }[] = [];
  server.use(
    http.get('*/api/v1/messages/m1/questions', () => HttpResponse.json({
      items: questions, drafts, question: item.question,
      available_directions: allowed ? ['mentor', 'hr'] : ['mentor'],
      routing: { direction: 'hr', reason: 'Вопрос об отпуске', clarification: null, split: false },
    })),
    http.put('*/api/v1/messages/m1/question-drafts/:direction', async ({ request,params }) => {
      const value = await request.json() as { question: string; context_excerpt: string };
      drafts = [{ ...value, direction: String(params.direction) }];
      return HttpResponse.json(drafts[0]);
    }),
    http.post('*/api/v1/messages/m1/questions', async ({ request }) => {
      const value = await request.json() as { question: string; context_excerpt: string;context_snapshot?:ContextInput };
      questions = [{ ...item, ...value,context_snapshot:approvedContext(value.context_snapshot) }];
      return HttpResponse.json(questions[0]);
    }),
    http.get('*/api/v1/questions/q1', () => HttpResponse.json({
      question: questions[0], history: [{ revision: 1, snapshot: { direction: 'hr', resolution: 'Старый ответ' } }],
    })),
    http.post('*/api/v1/questions/q1/reroute', async ({ request }) => {
      const value = await request.json() as { direction: string; question: string; context_excerpt: string;context_snapshot?:ContextInput };
      questions = [{ ...item, ...value,context_snapshot:approvedContext(value.context_snapshot), revision: 2 }];
      return HttpResponse.json(questions[0]);
    }),
  );
  render(<QueryClientProvider client={queryClient}><QuestionComposer messageId="m1" onOpenDocument={() => {}} /></QueryClientProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Обратиться к специалисту' }));
  expect(await screen.findByText('Для вашей команды пока не назначен HR.')).toBeVisible();
  expect(screen.getByRole('button', { name: 'Подтвердить отправку' })).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Контекст для специалиста'), { target: { value: 'Согласованный текст' } });
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' }));
  await waitFor(() => expect(drafts).toHaveLength(1));
  allowed = true;
  await queryClient.invalidateQueries({ queryKey: ['hr', 'message', 'm1'] });
  fireEvent.click(screen.getByRole('checkbox',{name:'Я проверил вопрос, адресата и весь отправляемый контекст'}));
  fireEvent.click(await screen.findByRole('button', { name: 'Подтвердить отправку' }));
  expect(await screen.findByText('HR · Новое')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'История' }));
  expect(await screen.findByText('Старый ответ')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Изменить адресата' }));
  expect(screen.getByLabelText('Контекст для специалиста')).toHaveValue('');
  fireEvent.click(screen.getByRole('button', { name: 'Отмена' }));
  fireEvent.click(screen.getByRole('button', { name: 'Изменить адресата' }));
  fireEvent.click(screen.getByRole('checkbox',{name:'Я проверил вопрос, адресата и весь отправляемый контекст'}));
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить перенаправление' }));
  expect(await screen.findByText('Наставник · Новое')).toBeVisible();
});

it('creates, previews, publishes, retries, edits and revokes Markdown', async () => {
  let current: typeof document | undefined;
  server.use(
    http.get('*/api/v1/hr/documents', () => HttpResponse.json({ items: current ? [current] : [] })),
    http.post('*/api/v1/hr/documents', async ({ request }) => {
      const body = await request.json() as Pick<typeof document, 'title' | 'content' | 'team_ids'>;
      current = { ...document, ...body };
      return HttpResponse.json(current);
    }),
    http.patch('*/api/v1/hr/documents/doc1', async ({ request }) => {
      const body = await request.json() as Pick<typeof document, 'title' | 'content' | 'team_ids' | 'version'>;
      current = { ...current!, ...body, version: body.version + 1, status: 'draft' };
      return HttpResponse.json(current);
    }),
    http.post('*/api/v1/hr/documents/doc1/:action', ({ params }) => {
      if (params.action === 'publish') current = { ...current!, status: 'failed', error: 'Сбой индексации' };
      if (params.action === 'retry') current = { ...current!, status: 'ready', published_version: current!.version, error: null };
      if (params.action === 'revoke') current = { ...current!, revoked: true };
      return HttpResponse.json(current);
    }),
  );
  mount('/hr/documents');
  expect(await screen.findByText('Инструкций пока нет.')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Новая инструкция' }));
  fireEvent.change(screen.getByLabelText('Загрузить Markdown'), { target: { files: [new File(['x'], 'x.txt')] } });
  expect(await screen.findByText('Выберите Markdown-файл размером до 1 МБ.')).toBeVisible();
  const file = new File(['# Отпуск'], 'Инструкция.md');
  Object.defineProperty(file, 'text', { value: async () => '# Отпуск' });
  fireEvent.change(screen.getByLabelText('Загрузить Markdown'), { target: { files: [file] } });
  await waitFor(() => expect(screen.getByLabelText('Название')).toHaveValue('Инструкция'));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Backend' }));
  fireEvent.click(screen.getByRole('button', { name: 'Предпросмотр' }));
  expect(screen.getByRole('heading', { name: 'Отпуск' })).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Опубликовать' }));
  expect(await screen.findByText('Сбой индексации')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Повторить индексацию' }));
  expect(await screen.findByText('Опубликован · версия 1')).toBeVisible();
  fireEvent.change(screen.getByLabelText('Текст Markdown'), { target: { value: 'Версия 2' } });
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' }));
  expect(await screen.findByText('Черновик · версия 2')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Отозвать публикацию' }));
  expect(await screen.findByText('Отозван · версия 2')).toBeVisible();
});

it('clears a private queue after access denial and a subsequent 503', async () => {
  let status = 200;
  server.use(http.get('*/api/v1/hr/questions', () => status === 200 ? HttpResponse.json({ items: [item] }) : missing(status)));
  mount('/hr/questions');
  expect(await screen.findByRole('link', { name: item.question })).toBeVisible();
  status = 403;
  await queryClient.invalidateQueries({ queryKey: ['hr', 'questions'] });
  expect(await screen.findByText('Недоступно')).toBeVisible();
  expect(screen.queryByRole('link', { name: item.question })).not.toBeInTheDocument();
  status = 503;
  fireEvent.click(screen.getByRole('button', { name: /повторить/i }));
  expect(screen.queryByRole('link', { name: item.question })).not.toBeInTheDocument();
});

it('keeps the HR route behind its role guard', async () => {
  const user = createScenario().user;
  const router = mount('/hr/documents', user);
  await waitFor(() => expect(router.state.location.pathname).toBe('/onboarding'));
  expect(screen.queryByRole('heading', { name: 'Кадровые инструкции' })).not.toBeInTheDocument();
});

it('rechecks the scope and discards a pending private request when assignments change', async () => {
  let version = '1';
  let release: (() => void) | undefined;
  let hits = 0;
  server.use(http.get('*/api/v1/hr/questions', async () => {
    hits++;
    if (hits === 2) {
      await new Promise<void>(resolve => { release = resolve; });
      return HttpResponse.json({ items: [item] });
    }
    return HttpResponse.json({ items: hits > 2 ? [] : [item] });
  }));
  mount('/hr/questions', employee, () => ({ version, teams: version === '1' ? [team] : [] }));
  expect(await screen.findByRole('link', { name: item.question })).toBeVisible();
  void queryClient.invalidateQueries({ queryKey: ['hr', 'questions'] });
  await waitFor(() => expect(release).toBeDefined());
  version = '2';
  await queryClient.invalidateQueries({ queryKey: ['session', 'access-scope'] });
  release?.();
  expect(await screen.findByText('Обращений пока нет.')).toBeVisible();
  expect(screen.queryByRole('link', { name: item.question })).not.toBeInTheDocument();
});

it('shows explicit login and navigation for a valid HR account', async () => {
  let loggedIn = false;
  server.use(
    http.get('*/api/v1/me', () => loggedIn ? HttpResponse.json(employee) : missing(401)),
    http.get('*/api/v1/demo/accounts', () => HttpResponse.json([employee])),
    http.post('*/api/v1/demo/session', () => { loggedIn = true; return HttpResponse.json(employee); }),
    http.get('*/api/v1/me/access-scope', () => HttpResponse.json({ version: '1', teams: [team] })),
    http.get('*/api/v1/hr/questions', () => HttpResponse.json({ items: [] })),
  );
  const router = createMemoryRouter(routes, { initialEntries: ['/login'] });
  render(<App routerOverride={router} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Войти как Анна HR' }));
  expect(await screen.findByRole('heading', { name: 'Кадровые обращения' })).toBeVisible();
  expect(router.state.location.pathname).toBe('/hr/questions');
});
