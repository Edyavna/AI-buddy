import { http, HttpResponse } from 'msw';
import { z } from 'zod';
import { createScenario } from '../fixtures/scenario';
import { createFullCheckpointPlan } from '../fixtures/checkpoints';
import { notFound } from './chat';
import { userSchema } from '../../features/session/schemas';
import { planSchema, setCheckpointConfirmationSchema } from '../../features/onboarding/schemas';
import type { Plan, Step } from '../../features/onboarding/schemas';
import { accessRequestSchema } from '../../features/access-requests/schemas';
import type { AccessRequest } from '../../features/access-requests/schemas';
import type { User } from '../../shared/api';

const createDemoSessionSchema = z.object({ account_id: z.string() }).strict();
const updateStepSchema = z.object({ status: z.enum(['not_started', 'in_progress', 'completed']) }).strict();
const confirmRequestSchema = z.object({ draft_version: z.number().int().min(1) }).strict();

const jsonError = (status: number, code: string, message: string) =>
  HttpResponse.json({ error: { code, message, trace_id: 'trace_mock' } }, { status });

const unauthorized = () => jsonError(401, 'UNAUTHORIZED', 'Сессия не найдена');

function withServerProgress(plan: Plan): Plan {
  const completed = plan.steps.filter((step) => step.status === 'completed').length;
  const next = plan.steps.find((step) => step.status !== 'completed' && step.status !== 'blocked');
  return {
    ...plan,
    progress_percent: plan.steps.length ? Math.floor((100 * completed) / plan.steps.length) : 0,
    next_step_id: next?.id ?? null,
  };
}

function withNextRevision(plan: Plan): Plan {
  const revision = Math.max(0, ...plan.steps.map((step) => step.revision)) + 1;
  return { ...plan, steps: plan.steps.map((step) => ({ ...step, revision })) };
}

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

export function createFe2Handlers(options: { plan?: Plan } = {}) {
  const fixture = createScenario();
  const intern = userSchema.parse(fixture.user);
  const mentor: User = {
    id: 'mentor_demo_1',
    name: 'Алексей Морозов',
    role: 'mentor',
    team: intern.team,
  };
  const accounts: User[] = [intern, mentor];

  let currentUser: User | null = intern;
  let plan: Plan = withServerProgress(planSchema.parse(options.plan ?? createFullCheckpointPlan()));
  let request: AccessRequest = accessRequestSchema.parse(fixture.draft);

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

    http.get('*/api/v1/me', () => currentUser ? HttpResponse.json(currentUser) : unauthorized()),

    http.delete('*/api/v1/session', () => {
      currentUser = null;
      return new HttpResponse(null, { status: 204 });
    }),

    http.get('*/api/v1/onboarding/plan', () => currentUser ? HttpResponse.json(plan) : unauthorized()),

    http.patch('*/api/v1/onboarding/steps/:stepId', async ({ params, request: incoming }) => {
      if (!currentUser) return unauthorized();
      if (currentUser.role !== 'intern') return jsonError(403, 'ROLE_FORBIDDEN', 'Действие доступно только стажёру');
      const parsed = updateStepSchema.safeParse(await incoming.json().catch(() => null));
      if (!parsed.success) return jsonError(422, 'VALIDATION_ERROR', 'Некорректный запрос');
      const step = plan.steps.find((item) => item.id === params.stepId);
      if (!step) return notFound();
      if (step.status === parsed.data.status) return HttpResponse.json(plan);
      if (!step.allowed_statuses.includes(parsed.data.status)) {
        return jsonError(409, 'STEP_TRANSITION_FORBIDDEN', step.blocked_reason ?? 'Шаг недоступен');
      }
      const checkpointCompletion = parsed.data.status === 'completed' || step.status === 'completed';
      const hasCheckpointPolicy = step.completion_policy !== 'intern' ||
        step.allowed_actions.length > 0 || step.confirmations.length > 0;
      if (checkpointCompletion && hasCheckpointPolicy) {
        return jsonError(409, 'STEP_TRANSITION_FORBIDDEN', 'Используйте подтверждение чекпоинта');
      }
      const steps = plan.steps.map((item): Step => item.id === step.id
        ? { ...item, status: parsed.data.status, allowed_statuses: allowedAfter(parsed.data.status) }
        : item);
      plan = withNextRevision(withServerProgress({ ...plan, steps }));
      return HttpResponse.json(plan);
    }),

    http.put('*/api/v1/onboarding/steps/:stepId/confirmation', async ({ params, request: incoming }) => {
      if (!currentUser) return unauthorized();
      if (currentUser.role !== 'intern') return jsonError(403, 'ROLE_FORBIDDEN', 'Действие доступно только стажёру');
      const parsed = setCheckpointConfirmationSchema.safeParse(await incoming.json().catch(() => null));
      if (!parsed.success) return jsonError(422, 'VALIDATION_ERROR', 'Некорректный запрос');
      const step = plan.steps.find((item) => item.id === params.stepId);
      if (!step) return notFound();
      if (step.completion_policy === 'mentor') {
        return jsonError(403, 'ROLE_FORBIDDEN', 'Этот чекпоинт подтверждает наставник');
      }

      const ownConfirmation = step.confirmations.find((item) => item.role === 'intern');
      if (Boolean(ownConfirmation) === parsed.data.confirmed) return HttpResponse.json(plan);
      if (parsed.data.revision !== step.revision) {
        return jsonError(409, 'CHECKPOINT_REVISION_CONFLICT', 'План изменился. Обновите его перед повтором.');
      }
      const action = parsed.data.confirmed ? 'confirm' : 'revoke';
      if (!step.allowed_actions.includes(action)) {
        return jsonError(409, 'STEP_TRANSITION_FORBIDDEN', step.blocked_reason ?? 'Действие сейчас недоступно');
      }

      const steps = plan.steps.map((item): Step => {
        if (item.id !== step.id) return item;
        const confirmations = item.confirmations.filter((confirmation) => confirmation.role !== 'intern');
        if (parsed.data.confirmed) {
          confirmations.push({
            role: 'intern',
            user_id: intern.id,
            confirmed_at: new Date().toISOString(),
          });
        }
        const internConfirmed = confirmations.some((confirmation) => confirmation.role === 'intern');
        const mentorConfirmed = confirmations.some((confirmation) => confirmation.role === 'mentor');
        const completed = item.completion_policy === 'both'
          ? internConfirmed && mentorConfirmed
          : internConfirmed;
        const awaitingRole = item.completion_policy === 'both' && internConfirmed !== mentorConfirmed
          ? (internConfirmed ? 'mentor' : 'intern')
          : null;
        const stillBlocked = item.status === 'blocked' && !parsed.data.confirmed;
        return {
          ...item,
          status: stillBlocked ? 'blocked' : completed ? 'completed' : 'in_progress',
          confirmations,
          awaiting_role: awaitingRole,
          allowed_actions: stillBlocked && !internConfirmed ? [] : [internConfirmed ? 'revoke' : 'confirm'],
          allowed_statuses: completed ? [] : item.allowed_statuses,
        };
      });
      plan = withNextRevision(withServerProgress({ ...plan, steps }));
      return HttpResponse.json(plan);
    }),

    http.get('*/api/v1/access-requests/:id', ({ params }) => {
      if (!currentUser) return unauthorized();
      if (params.id !== request.id) return notFound();
      if (request.status === 'submitting') {
        request = accessRequestSchema.parse(fixture.pending);
      } else if (request.status === 'pending') {
        request = accessRequestSchema.parse(fixture.granted);
      }
      if (request.status === 'granted') {
        const grantedStep = planSchema.parse(fixture.granted_plan).steps[0]!;
        plan = withNextRevision(withServerProgress({
          ...plan,
          steps: plan.steps.map((step) => step.id === grantedStep.id ? grantedStep : step),
        }));
      }
      return HttpResponse.json(request);
    }),

    http.post('*/api/v1/access-requests/:id/confirm', async ({ params, request: incoming }) => {
      if (!currentUser) return unauthorized();
      if (params.id !== request.id) return notFound();
      const parsed = confirmRequestSchema.safeParse(await incoming.json().catch(() => null));
      if (!parsed.success) return jsonError(422, 'VALIDATION_ERROR', 'Некорректный запрос');
      if (request.status === 'draft' || request.status === 'failed') {
        if (parsed.data.draft_version !== request.draft_version) {
          return jsonError(409, 'DRAFT_CHANGED', 'Параметры заявки изменились, подтвердите новую версию');
        }
        request = accessRequestSchema.parse({ ...fixture.submitting, draft_version: request.draft_version });
        return HttpResponse.json(request, { status: 202 });
      }
      if (request.status === 'submitting') return HttpResponse.json(request, { status: 202 });
      return HttpResponse.json(request, { status: 200 });
    }),
  ];
}
