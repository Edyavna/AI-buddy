import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { afterEach, expect, it, vi } from 'vitest';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { abortSessionRequests } from '@/shared/api/session-signal';
import { ResolutionDraftPage } from './ResolutionDraftPage';
import { draftKey, resolutionApi, type Direction, type Draft, type Workspace, type SaveDraft } from './api';

const source = { id: 'q', direction: 'mentor' as Direction, question: 'Как настроить сервис?', resolution: 'Проверьте конфигурацию', team_id: 'Backend', step_id: 'step-1', revision: 3 };
const draft = (overrides: Partial<Draft> = {}): Draft => ({ revision: 1, title: 'Инструкция', content: 'Проверенный текст', target_document_id: null, base_version: null, source, team_ids: ['Backend'], base_content: '', document_id: null, document_version: null, status: 'draft', error: null, ...overrides });
const failure = (status: number) => HttpResponse.json({ error: { code: 'TEST_ERROR', message: status === 409 ? 'Версия изменилась' : 'Сервис недоступен', trace_id: 'test' } }, { status });

afterEach(() => vi.restoreAllMocks());
function setup(initial: Draft | null = null, direction: Direction = 'mentor') {
  const state: Workspace = { source: { ...source, direction }, draft: initial, targets: [{ id: 'target', title: 'Существующий документ', version: 4, content: '', team_ids: ['Backend'] }] };
  const root = `*/api/v1/${direction}/questions/q/instruction-draft`;
  let saves = 0, publications = 0;
  server.use(
    http.get('*/api/v1/me/access-scope', () => HttpResponse.json({ version: '1', teams: [{ id: 'Backend', name: 'Команда разработки' }] })),
    http.get(root, () => HttpResponse.json(state)),
    http.get(`${root}/targets/target`, () => HttpResponse.json({ ...state.targets[0], content: 'Исходная инструкция' })),
    http.put(root, async ({ request }) => {
      const body = await request.json() as SaveDraft; saves++;
      state.draft = draft({ ...body, revision: body.revision + 1, source: state.source, base_content: body.target_document_id ? 'Исходная инструкция' : '' });
      return HttpResponse.json(state.draft);
    }),
    http.post(`${root}/generate`, () => HttpResponse.json({ title: 'Предложение', content: '**Проверить настройки**\n<script>alert(1)</script>', mode: 'demo' })),
    ...['publish', 'retry'].map(action => http.post(`${root}/${action}`, async ({ request }) => {
      publications++;
      expect(await request.json()).toEqual({ revision: state.draft?.revision, reviewed: true });
      state.draft = draft({ ...state.draft, document_id: 'published', document_version: 1, status: 'indexing', error: null });
      return HttpResponse.json(state.draft);
    })),
    http.get('*/api/v1/mentor/documents/published', () => HttpResponse.json({ id: 'published', title: 'Инструкция', content: 'Готовый документ', status: 'ready', author: { id: 'mentor', name: 'Наставник' }, question_id: 'q', error: null, created_at: '2026-11-13T10:00:00Z', updated_at: '2026-11-13T10:00:00Z' })),
    http.get('*/api/v1/documents/published', () => HttpResponse.json({ id: 'published', title: 'Инструкция', content: 'Готовый документ', updated_at: '2026-11-13T10:00:00Z' })),
  );
  const router = createMemoryRouter([{ path: '/:direction/questions/:id/instruction', element: <ResolutionDraftPage direction={direction} /> }], { initialEntries: [`/${direction}/questions/q/instruction`] });
  return { ...renderWithQuery(<RouterProvider router={router} />), state, root, counts: () => ({ saves, publications }) };
}
async function saved() { await screen.findByText('Все изменения сохранены'); }
function edit(text = 'Инструкция вручную') { fireEvent.change(screen.getByLabelText('Текст инструкции'), { target: { value: text } }); }
async function reviewAndPublish() {
  fireEvent.click(screen.getByRole('checkbox'));
  fireEvent.click(screen.getByRole('button', { name: 'Опубликовать инструкцию' }));
  await screen.findByText('Публикация принята. Ссылка появится в вопросе после индексации.');
}

it.each<Direction>(['mentor', 'hr'])('saves and publishes only after editor review, then shows a ready document for %s', async direction => {
  const { state, counts, queryClient } = setup(null, direction);
  await screen.findByRole('heading', { name: 'Редактор Markdown' });
  expect(screen.getByRole('button', { name: 'Опубликовать инструкцию' })).toBeDisabled();
  fireEvent.click(screen.getByText(/Источник:/)); expect(screen.getByText('Проверьте конфигурацию')).toBeInTheDocument();
  edit(); fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' })); await saved();
  expect(counts()).toEqual({ saves: 1, publications: 0 });
  await reviewAndPublish();
  expect(screen.queryByRole('heading', { name: 'Редактор Markdown' })).not.toBeInTheDocument();
  state.draft = draft({ ...state.draft, status: 'ready' });
  await queryClient.invalidateQueries({ queryKey: draftKey(direction, 'q') });
  fireEvent.click(await screen.findByRole('button', { name: 'Открыть опубликованную инструкцию' }));
  const modal = await screen.findByRole('dialog'); expect(await within(modal).findByText('Готовый документ')).toBeInTheDocument();
  fireEvent.click(within(modal).getByRole('button', { name: /Закрыть/ }));
});
it('restores the saved draft, previews an append and can select a new document again', async () => {
  setup(draft()); await saved();
  expect(screen.getByLabelText('Текст инструкции')).toHaveValue('Проверенный текст');
  fireEvent.change(screen.getByLabelText('Куда опубликовать'), { target: { value: 'target' } });
  expect(await screen.findByLabelText('Дополнение к документу')).toHaveValue('Проверенный текст');
  fireEvent.click(screen.getByText('Существующий текст · версия 4'));
  expect(screen.getByText('Исходная инструкция')).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Название инструкции'), { target: { value: 'Новое название' } });
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' })); await saved();
  fireEvent.change(screen.getByLabelText('Куда опубликовать'), { target: { value: '' } });
  expect(await screen.findByLabelText('Текст инструкции')).toBeInTheDocument();
});
it('generates a safe preview and preserves manual text when generation fails', async () => {
  const { root } = setup(); await screen.findByLabelText('Текст инструкции');
  fireEvent.click(screen.getByRole('button', { name: 'Предложить текст' }));
  expect(await screen.findByText('Демонстрационное предложение. Проверьте и отредактируйте текст.')).toBeInTheDocument();
  expect(screen.getByText('Проверить настройки')).toBeInTheDocument(); expect(document.querySelector('script')).toBeNull();
  server.use(http.post(`${root}/generate`, () => failure(503)));
  fireEvent.click(screen.getByRole('button', { name: 'Заменить текст предложением помощника' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Сервис недоступен');
  expect(screen.getByLabelText('Текст инструкции')).toHaveValue('**Проверить настройки**\n<script>alert(1)</script>');
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' })); await saved();
});
it('labels live model proposals and resets review after edits', async () => {
  const { root } = setup(draft()); await saved();
  server.use(http.post(`${root}/generate`, () => HttpResponse.json({ title: 'Инструкция', content: 'Текст модели', mode: 'model' })));
  fireEvent.click(screen.getByRole('button', { name: 'Заменить текст предложением помощника' }));
  await screen.findByText('Предложение помощника. Проверьте и отредактируйте текст.');
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' })); await saved();
  fireEvent.click(screen.getByRole('checkbox')); edit('Другой текст'); expect(screen.getByRole('checkbox')).not.toBeChecked();
});
it('shows failed indexing and explicitly retries the same publication', async () => {
  const { counts } = setup(draft({ status: 'failed', document_id: 'published', document_version: 1, error: 'Ошибка провайдера' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Ошибка провайдера');
  fireEvent.click(screen.getByRole('button', { name: 'Повторить индексацию' }));
  await screen.findByText('Публикация принята. Ссылка появится в вопросе после индексации.'); expect(counts().publications).toBe(1);
});
it.each([401, 403, 404])('removes editor and preview after %i and keeps them hidden through a subsequent 503', async status => {
  const { root, queryClient } = setup(draft()); await saved();
  server.use(http.get(root, () => failure(status)));
  await queryClient.invalidateQueries({ queryKey: draftKey('mentor', 'q') });
  await waitFor(() => expect(screen.queryByLabelText('Текст инструкции')).not.toBeInTheDocument());
  server.use(http.get(root, () => failure(503))); await queryClient.invalidateQueries({ queryKey: draftKey('mentor', 'q') });
  expect(screen.queryByText('Проверенный текст')).not.toBeInTheDocument();
});
it('revokes the entire draft workspace when a mutation loses access', async () => {
  const { root } = setup(draft()); await saved();
  server.use(http.put(root, () => failure(403))); edit(); fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' }));
  await waitFor(() => expect(screen.queryByLabelText('Текст инструкции')).not.toBeInTheDocument());
});
it('reconciles a lost save response before reporting success', async () => {
  const { state, root } = setup(); await screen.findByLabelText('Текст инструкции');
  server.use(http.put(root, async ({ request }) => { const body = await request.json() as SaveDraft; state.draft = draft({ ...body, revision: 1 }); return HttpResponse.error(); }));
  edit(); fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' })); await saved(); expect(state.draft?.revision).toBe(1);
});
it('does not report a save if reconciliation finds a different draft; explicit refresh preserves manual text', async () => {
  const { root } = setup(draft()); await saved();
  server.use(http.put(root, () => HttpResponse.error())); edit(); fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' }));
  await screen.findByText('Результат операции пока неизвестен. Сначала сверьте состояние с сервером.');
  expect(screen.getByLabelText('Текст инструкции')).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Сверить с сервером' }));
  await screen.findByText('Версия перечитана. Ваш текст сохранён в редакторе. Проверьте его перед новой записью.');
  expect(screen.getByLabelText('Текст инструкции')).toHaveValue('Инструкция вручную');
});
it('reconciles lost publish and retry responses with the durable job', async () => {
  const { root, state } = setup(draft()); await saved();
  server.use(http.post(`${root}/publish`, () => { state.draft = draft({ status: 'indexing', document_id: 'published', document_version: 1 }); return failure(503); }));
  await reviewAndPublish();
});
it('keeps an unknown publish locked when the job is absent, then checks again explicitly', async () => {
  const { root } = setup(draft()); await saved();
  server.use(http.post(`${root}/publish`, () => HttpResponse.error()));
  fireEvent.click(screen.getByRole('checkbox')); fireEvent.click(screen.getByRole('button', { name: 'Опубликовать инструкцию' }));
  await screen.findByText('Результат операции пока неизвестен. Сначала сверьте состояние с сервером.');
  fireEvent.click(screen.getByRole('button', { name: 'Сверить с сервером' })); await screen.findByText(/Версия перечитана/);
});
it('requires refresh after version conflict and never silently retries', async () => {
  const { root, counts } = setup(draft({ target_document_id: 'target', base_version: 3, base_content: 'Старый текст' })); await saved();
  server.use(http.post(`${root}/publish`, () => failure(409)));
  fireEvent.click(screen.getByRole('checkbox')); fireEvent.click(screen.getByRole('button', { name: 'Опубликовать инструкцию' }));
  await screen.findByText(/автоматической перезаписи не будет/); expect(counts().publications).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Обновить исходный документ' }));
  await screen.findByText('Существующий текст · версия 4');
  expect(screen.getByLabelText('Название инструкции')).toHaveValue('Инструкция');
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' })); await saved();
});
it('ignores a late successful generation after logout', async () => {
  const { queryClient } = setup(draft()); await saved();
  let resolve!: (v: { title: string; content: string; mode: 'demo' }) => void;
  vi.spyOn(resolutionApi, 'generate').mockImplementation(() => new Promise(r => { resolve = r; }));
  fireEvent.click(screen.getByRole('button', { name: 'Заменить текст предложением помощника' }));
  await waitFor(() => expect(resolve).toBeDefined()); abortSessionRequests(queryClient); resolve({ title: 'Чужой ответ', content: 'Секрет', mode: 'demo' });
  await new Promise(r => setTimeout(r, 20)); expect(screen.getByLabelText('Текст инструкции')).toHaveValue('Проверенный текст');
});
it('does not restore a late successful save after revoked access and a later 503', async () => {
  const { root, queryClient } = setup(draft()); await saved();
  let resolve!: (value: Draft) => void;
  vi.spyOn(resolutionApi, 'save').mockImplementation(() => new Promise(r => { resolve = r; }));
  edit(); fireEvent.click(screen.getByRole('button', { name: 'Сохранить черновик' }));
  await waitFor(() => expect(resolve).toBeDefined());
  server.use(http.get(root, () => failure(403)));
  await queryClient.invalidateQueries({ queryKey: draftKey('mentor', 'q') });
  await waitFor(() => expect(screen.queryByLabelText('Текст инструкции')).not.toBeInTheDocument());
  resolve(draft({ revision: 2, title: 'Поздний ответ', content: 'Закрытый текст' }));
  server.use(http.get(root, () => failure(503)));
  await queryClient.invalidateQueries({ queryKey: draftKey('mentor', 'q') });
  expect(queryClient.getQueryData(draftKey('mentor', 'q'))).toBeNull();
  expect(screen.queryByText('Закрытый текст')).not.toBeInTheDocument();
});
