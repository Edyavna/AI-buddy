import { http, HttpResponse } from 'msw';
import { z } from 'zod';
import { createScenario } from '../fixtures/scenario';
import { notFound } from './chat';
import { userSchema } from '../../features/session/schemas';
import { planSchema } from '../../features/onboarding/schemas';
import type { Plan, Step } from '../../features/onboarding/schemas';
import { accessRequestSchema } from '../../features/access-requests/schemas';
import type { AccessRequest } from '../../features/access-requests/schemas';
import type { User } from '../../shared/api';

const createDemoSessionSchema = z.object({ account_id: z.string() }).strict();
const updateStepSchema = z
  .object({ status: z.enum(['not_started', 'in_progress', 'completed']) })
  .strict();

const jsonError = (status: number, code: string, message: string) =>
  HttpResponse.json({ error: { code, message, trace_id: 'trace_mock' } }, { status });

const unauthorized = () => jsonError(401, 'UNAUTHORIZED', 'Сессия не найдена');

// Server-side plan rule (OpenAPI Plan): floor(100 * completed / total).
function withServerProgress(plan: Plan): Plan {
  const completed = plan.steps.filter((step) => step.status === 'completed').length;
  const next = plan.steps.find((step) => step.status !== 'completed' && step.status !== 'blocked');
  return {
    ...plan,
    progress_percent: plan.steps.length ? Math.floor((100 * completed) / plan.steps.length) : 0,
    next_step_id: next?.id ?? null,
  };
}

// Allowed transitions after a user change, mirroring the canonical fixtures.
function allowedAfter(status: Step['status']): Step['allowed_statuses'] {
  switch (status) {
    case 'in_progress':
      return ['not_started', 'completed'];
    case 'completed':
      return ['in_progress'];
    default:
      return ['in_progress'];
  }
}

// FE2-owned handlers (session, plan, access requests) behind one seam.
// State lives inside the factory so each test/dev run starts clean.
export function createFe2Handlers() {
  const fixture = createScenario();
  // JSON fixtures are loosely typed: validate at the boundary and keep enums narrow.
  const intern = userSchema.parse(fixture.user);
  const mentor: User = {
    id: 'mentor_demo_1',
    name: 'Алексей Морозов',
    role: 'mentor',
    team: intern.team,
  };
  const accounts: User[] = [intern, mentor];

  let currentUser: User | null = intern;
  let plan: Plan = withServerProgress(planSchema.parse(fixture.plan));
  const request: AccessRequest = accessRequestSchema.parse(fixture.draft);

  return [
    http.get('*/api/v1/demo/accounts', () => HttpResponse.json(accounts)),

    http.post('*/api/v1/demo/session', async ({ request: incoming }) => {
      const parsed = createDemoSessionSchema.safeParse(await incoming.json().catch(() => null));
      if (!parsed.success) return jsonError(422, 'VALIDATION_ERROR', 'Некорректный запрос');
      const account = accounts.find((item) => item.id === parsed.data.account_id);
      if (!account) return jsonError(404, 'NOT_FOUND', 'Тестовая учетная запись не найдена');
      currentUser = account;
      return HttpResponse.json(account);
    }),

    http.get('*/api/v1/me', () =>
      currentUser ? HttpResponse.json(currentUser) : unauthorized()),

    http.delete('*/api/v1/session', () => {
      currentUser = null;
      return new HttpResponse(null, { status: 204 });
    }),

    http.get('*/api/v1/onboarding/plan', () =>
      currentUser ? HttpResponse.json(plan) : unauthorized()),

    http.patch('*/api/v1/onboarding/steps/:stepId', async ({ params, request: incoming }) => {
      if (!currentUser) return unauthorized();
      const parsed = updateStepSchema.safeParse(await incoming.json().catch(() => null));
      if (!parsed.success) return jsonError(422, 'VALIDATION_ERROR', 'Некорректный запрос');
      const step = plan.steps.find((item) => item.id === params.stepId);
      if (!step) return notFound();
      // Repeating the current user status is a 200 no-op (OpenAPI updateStep).
      if (step.status === parsed.data.status) return HttpResponse.json(plan);
      if (!step.allowed_statuses.includes(parsed.data.status)) {
        return jsonError(409, 'STEP_BLOCKED', step.blocked_reason ?? 'Шаг недоступен');
      }
      const steps = plan.steps.map((item): Step =>
        item.id === step.id
          ? { ...item, status: parsed.data.status, allowed_statuses: allowedAfter(parsed.data.status) }
          : item,
      );
      plan = withServerProgress({ ...plan, steps });
      return HttpResponse.json(plan);
    }),

    http.get('*/api/v1/access-requests/:id', ({ params }) => {
      if (!currentUser) return unauthorized();
      return params.id === request.id ? HttpResponse.json(request) : notFound();
    }),
  ];
}
