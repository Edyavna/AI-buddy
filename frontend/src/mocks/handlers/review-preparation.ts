import { http, HttpResponse } from 'msw';
import { z } from 'zod';
import { mockSession } from './session-bus';
import type { ReviewCheck, ReviewPreparation } from '../../features/review-preparation/schemas';

const jsonError = (status: number, code: string, message: string) =>
  HttpResponse.json({ error: { code, message, trace_id: 'review_mock' } }, { status });

const unauthorized = () => jsonError(401, 'UNAUTHENTICATED', 'Войдите в систему');
const forbidden = () => jsonError(403, 'ROLE_FORBIDDEN', 'Действие недоступно для этой роли');
const notFound = () => jsonError(404, 'NOT_FOUND', 'Объект недоступен или удалён');
const changed = () => jsonError(409, 'REVIEW_CHANGED', 'Подготовка уже изменилась, обновите страницу');

const defaultItems = [
  { key: 'task_done', label: 'Код решает задачу целиком', required: true },
  { key: 'tests_added', label: 'Добавлены или обновлены тесты', required: true },
  { key: 'checks_green', label: 'Проверки CI проходят на head-коммите', required: true },
  { key: 'docs_updated', label: 'Документация и описание PR обновлены', required: false },
  { key: 'no_secrets', label: 'Нет секретов, отладочного кода и лишних файлов', required: true },
];

const prURLPattern = /^https?:\/\/github\.com\/([\w.-]+)\/([\w.-]+)\/pull\/(\d+)\/?$/;
const createSchema = z.object({ pr_url: z.string(), step_id: z.string().nullable().optional(), manual: z.boolean().optional() }).strict();
const updateSchema = z.object({
  revision: z.number().int().min(1),
  description: z.string(),
  answers: z.object({
    summary: z.string(),
    verification: z.string(),
    limitations: z.string(),
    questions: z.string(),
    items: z.record(z.string(), z.object({ done: z.boolean(), note: z.string() })),
  }),
  checks: z.array(z.object({ name: z.string(), status: z.enum(['passed', 'failed', 'skipped']), detail: z.string() })),
}).strict();
const revisionSchema = z.object({ revision: z.number().int().min(1) }).strict();
const templateSchema = z.object({
  items: z.array(z.object({ key: z.string().min(1), label: z.string().min(1), required: z.boolean() })).min(1).max(50),
  revision: z.number().int().min(0),
}).strict();

let sequence = 0;
const nextId = () => `prep_${++sequence}`;
const now = () => new Date().toISOString();

function emptyAnswers() {
  return { summary: '', verification: '', limitations: '', questions: '', items: {} as Record<string, { done: boolean; note: string }> };
}

function snapshot(prep: ReviewPreparation) {
  return {
    id: prep.id, status: prep.status, revision: prep.revision, step_id: prep.step_id, pr: prep.pr,
    description: prep.description, answers: prep.answers, checks: prep.checks,
    history: prep.history, created_at: prep.created_at, updated_at: prep.updated_at, submitted_at: prep.submitted_at,
  };
}

// Стаб интеграции GitHub: смена коммита при каждом обновлении, CI фиксируется на коммите.
const commits = ['a1b2c3d', 'd4e5f6a', 'b7c8d9e'];
function githubInfo(sha: string) {
  return {
    title: 'Добавить онбординг нового стажёра',
    head_sha: sha,
    checks: [
      { name: 'build', status: 'passed' as const },
      { name: 'lint', status: 'failed' as const },
      { name: 'e2e', status: 'skipped' as const },
    ],
  };
}

// Полное поведение фичи без бэкенда: черновик, отправка, история, CI, права ролей.
export function createReviewPreparationHandlers() {
  let refreshCount = 0;
  let template = { items: structuredClone(defaultItems), revision: 0, updated_at: now(), updated_by: '', custom: false };
  const preps: ReviewPreparation[] = [];
  const intern = () => {
    const user = mockSession.get();
    return user?.role === 'intern' ? user : null;
  };
  const mentor = () => {
    const user = mockSession.get();
    return user?.role === 'mentor' ? user : null;
  };

  return [
    http.get('*/api/v1/review-preparations/template', () => HttpResponse.json(template)),

    http.put('*/api/v1/review-preparations/template', async ({ request }) => {
      if (!mockSession.get()) return unauthorized();
      if (!mentor()) return forbidden();
      const body = templateSchema.safeParse(await request.json().catch(() => null));
      if (!body.success) return jsonError(422, 'VALIDATION_ERROR', 'Проверьте поля шаблона');
      if (body.data.revision !== template.revision) return changed();
      template = { items: body.data.items, revision: template.revision + 1, updated_at: now(), updated_by: mentor()!.id, custom: true };
      return HttpResponse.json(template);
    }),

    http.get('*/api/v1/review-preparations', () => {
      if (!intern()) return mockSession.get() ? forbidden() : unauthorized();
      const draft = preps.find(p => p.status === 'draft');
      const latest = draft ?? [...preps].sort((a, b) => b.updated_at.localeCompare(a.updated_at))[0] ?? null;
      return HttpResponse.json({ item: latest });
    }),

    http.post('*/api/v1/review-preparations', async ({ request }) => {
      if (!intern()) return mockSession.get() ? forbidden() : unauthorized();
      const body = createSchema.safeParse(await request.json().catch(() => null));
      if (!body.success) return jsonError(422, 'VALIDATION_ERROR', 'Проверьте поля запроса');
      const match = prURLPattern.exec(body.data.pr_url.trim());
      if (!match) return HttpResponse.json({ error: { code: 'VALIDATION_ERROR', message: 'Проверьте поля запроса', trace_id: 'review_mock', field_errors: { pr_url: 'Поддерживается только github.com/владелец/репозиторий/pull/номер' } } }, { status: 422 });
      if (preps.some(p => p.status === 'draft')) return jsonError(409, 'REVIEW_DRAFT_EXISTS', 'Черновик подготовки уже существует, обновите его');
      const manual = body.data.manual ?? false;
      const info = manual ? { title: '', head_sha: '', checks: [] } : githubInfo(commits[0]!);
      const prep: ReviewPreparation = {
        id: nextId(),
        status: 'draft',
        revision: 1,
        step_id: body.data.step_id ?? null,
        pr: {
          url: body.data.pr_url.trim(), repo: `${match[1]}/${match[2]}`, number: Number(match[3]),
          title: info.title, head_sha: info.head_sha, source: manual ? 'manual' : 'github',
          fetch_error: '', fetched_at: manual ? null : now(),
        },
        description: '',
        answers: emptyAnswers(),
        checks: info.checks.map((check, index) => ({ id: `ci_${index}`, name: check.name, status: check.status, source: 'ci', detail: '', created_at: now() })),
        history: [],
        created_at: now(),
        updated_at: now(),
        submitted_at: null,
      };
      preps.push(prep);
      return HttpResponse.json(prep, { status: 201 });
    }),

    http.get('*/api/v1/review-preparations/:id', ({ params }) => {
      if (!intern()) return mockSession.get() ? forbidden() : unauthorized();
      const prep = preps.find(p => p.id === params.id);
      return prep ? HttpResponse.json(prep) : notFound();
    }),

    http.put('*/api/v1/review-preparations/:id', async ({ params, request }) => {
      if (!intern()) return mockSession.get() ? forbidden() : unauthorized();
      const prep = preps.find(p => p.id === params.id);
      if (!prep) return notFound();
      const body = updateSchema.safeParse(await request.json().catch(() => null));
      if (!body.success) return jsonError(422, 'VALIDATION_ERROR', 'Проверьте поля запроса');
      if (body.data.revision !== prep.revision) return changed();
      const manualChecks = body.data.checks.filter(check => check.name.trim());
      const existingChecks = prep.checks.filter(check => check.source === 'intern');
      const sameChecks = manualChecks.length === existingChecks.length && manualChecks.every((check, index) => {
        const existing = existingChecks[index];
        return check.name === existing.name && check.status === existing.status && check.detail === existing.detail;
      });
      if (prep.description === body.data.description &&
          JSON.stringify(prep.answers) === JSON.stringify(body.data.answers) && sameChecks) {
        return HttpResponse.json(prep);
      }
      prep.revision += 1;
      prep.description = body.data.description;
      prep.answers = body.data.answers;
      prep.checks = [...prep.checks.filter(c => c.source === 'ci'),
        ...manualChecks.map((check, index) => ({ id: `manual_${prep.revision}_${index}`, name: check.name, status: check.status, source: 'intern' as const, detail: check.detail, created_at: now() }))];
      prep.updated_at = now();
      return HttpResponse.json(prep);
    }),

    http.post('*/api/v1/review-preparations/:id/submit', async ({ params, request }) => {
      if (!intern()) return mockSession.get() ? forbidden() : unauthorized();
      const prep = preps.find(p => p.id === params.id);
      if (!prep) return notFound();
      const body = revisionSchema.safeParse(await request.json().catch(() => null));
      if (!body.success) return jsonError(422, 'VALIDATION_ERROR', 'Проверьте поля запроса');
      if (body.data.revision !== prep.revision) return changed();
      prep.history = [...prep.history, { revision: prep.revision, submitted_at: now() }];
      prep.status = 'submitted';
      prep.revision += 1;
      prep.submitted_at = now();
      prep.updated_at = now();
      return HttpResponse.json(prep);
    }),

    http.post('*/api/v1/review-preparations/:id/assistant', ({ params }) => {
      if (!intern()) return mockSession.get() ? forbidden() : unauthorized();
      const prep = preps.find(p => p.id === params.id);
      if (!prep) return notFound();
      const missing = template.items.filter(item => item.required && !prep.answers.items[item.key]?.done).map(item => item.label);
      if (!prep.answers.summary.trim()) missing.push('Резюме: что сделано');
      if (!prep.answers.verification.trim()) missing.push('Как проверено');
      if (!prep.checks.length) missing.push('Результаты проверок с источником');
      const summary = `## ${prep.pr.title || `PR #${prep.pr.number}`}\n\n### Что сделано\n${prep.answers.summary || '—'}\n\n### Как проверено\n${prep.answers.verification || '—'}${missing.length ? `\n\n### Стоит дополнить\n${missing.map(item => `- ${item}`).join('\n')}` : ''}`;
      return HttpResponse.json({ summary, missing });
    }),

    http.post('*/api/v1/review-preparations/:id/refresh', ({ params }) => {
      if (!intern()) return mockSession.get() ? forbidden() : unauthorized();
      const prep = preps.find(p => p.id === params.id);
      if (!prep) return notFound();
      if (prep.pr.source !== 'github') return jsonError(409, 'REVIEW_REFRESH_UNAVAILABLE', 'Обновление сведений доступно только при интеграции GitHub');
      const current = commits.indexOf(prep.pr.head_sha);
      refreshCount += 1;
      // Первое обновление меняет коммит, дальше сведения актуальны.
      const info = githubInfo(refreshCount === 1 ? commits[(current + 1) % commits.length]! : prep.pr.head_sha);
      const changedCommit = prep.pr.head_sha !== info.head_sha;
      prep.pr = { ...prep.pr, title: info.title, head_sha: info.head_sha, fetch_error: '', fetched_at: now() };
      prep.checks = [...prep.checks.filter(c => c.source === 'intern'),
        ...info.checks.map((check, index): ReviewCheck => ({ id: `ci_${Date.now()}_${index}`, name: check.name, status: check.status, source: 'ci', detail: '', created_at: now() }))];
      prep.updated_at = now();
      return HttpResponse.json({ preparation: prep, changed: changedCommit });
    }),

    http.get('*/api/v1/mentor/review-preparations', ({ request }) => {
      if (!mentor()) return mockSession.get() ? forbidden() : unauthorized();
      const internId = new URL(request.url).searchParams.get('intern_id') ?? '';
      const student = mockSession.lastIntern();
      const items = preps
        .filter(() => !internId || student?.id === internId)
        .map(p => ({ ...snapshot(p), student: { id: student?.id ?? 'intern', name: student?.name ?? 'Стажёр' } }));
      return HttpResponse.json({ items });
    }),

    http.get('*/api/v1/mentor/review-preparations/:id', ({ params }) => {
      if (!mentor()) return mockSession.get() ? forbidden() : unauthorized();
      const prep = preps.find(p => p.id === params.id);
      return prep ? HttpResponse.json(prep) : notFound();
    }),
  ];
}
