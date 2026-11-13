import { afterEach, describe, expect, it, vi } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { acceptPlan, handlePlanAccessError } from './cache';
import { setCheckpointConfirmationShared } from './confirm';
import { onboardingApi } from './api';
import { planSchema } from './schemas';
import type { Plan } from './schemas';
import { ClientError, queryKeys } from '@/shared/api';
import { resetSessionState } from '@/features/session/session-state';
import { queryRequestSignal } from '@/shared/api/query-access';

function planAt(revision: number): Plan {
  return planSchema.parse({
    id: 'plan_checkpoint',
    title: 'План',
    progress_percent: revision > 1 ? 100 : 0,
    next_step_id: revision > 1 ? null : 'step_checkpoint',
    steps: [{
      id: 'step_checkpoint',
      title: 'Шаг',
      description: 'Описание',
      status: revision > 1 ? 'completed' : 'in_progress',
      request_ids: [],
      allowed_statuses: [],
      blocked_reason: null,
      template_key: 'checkpoint',
      period: null,
      completion_policy: 'intern',
      confirmations: revision > 1 ? [{ role: 'intern', user_id: 'intern_demo_1', confirmed_at: '2026-11-12T12:00:00Z' }] : [],
      revision,
      allowed_actions: revision > 1 ? ['revoke'] : ['confirm'],
      awaiting_role: null,
      instructions: 'Инструкция',
      resources: [],
    }],
  });
}

const clients: QueryClient[] = [];
afterEach(() => {
  vi.restoreAllMocks();
  clients.splice(0).forEach((client) => client.clear());
});

function client() {
  const value = new QueryClient();
  clients.push(value);
  return value;
}

describe('checkpoint cache lifecycle', () => {
  it('does not let an older plan response replace a newer revision', () => {
    const queryClient = client();
    const current = planAt(2);
    const signal = queryRequestSignal(queryClient, queryKeys.plan());
    acceptPlan(queryClient, current, signal);
    expect(acceptPlan(queryClient, planAt(1), signal)).toEqual(current);
    expect(queryClient.getQueryData(queryKeys.plan())).toEqual(current);
  });

  it('does not confirm an uncertain revoke when the refreshed plan omits the step', async () => {
    const queryClient = client();
    vi.spyOn(onboardingApi, 'setCheckpointConfirmation').mockRejectedValue(new ClientError('network', 'Сеть недоступна'));
    vi.spyOn(onboardingApi, 'getMyPlan').mockResolvedValue({ status: 200, data: { ...planAt(2), steps: [] } });

    await expect(setCheckpointConfirmationShared(queryClient, 'step_checkpoint', false, 2))
      .rejects.toThrow('Исход действия не подтверждён');
  });

  it.each([
    [new ClientError('network', 'Сеть недоступна'), 'Исход действия не подтверждён'],
    [new ClientError('http', 'Конфликт', { status: 409, code: 'CHECKPOINT_REVISION_CONFLICT' }), 'План изменился'],
  ])('explains the mutation outcome when the follow-up read fails', async (error, message) => {
    vi.spyOn(onboardingApi, 'setCheckpointConfirmation').mockRejectedValue(error);
    vi.spyOn(onboardingApi, 'getMyPlan').mockRejectedValue(new ClientError('http', 'Недоступен', { status: 503 }));

    await expect(setCheckpointConfirmationShared(client(), 'step_checkpoint', false, 2))
      .rejects.toThrow(message);
  });

  it('deduplicates concurrent operations for the same checkpoint', async () => {
    const queryClient = client();
    let release!: (plan: Plan) => void;
    const gate = new Promise<Plan>((resolve) => { release = resolve; });
    const call = vi.spyOn(onboardingApi, 'setCheckpointConfirmation').mockImplementation(async () => ({ status: 200, data: await gate }));

    const first = setCheckpointConfirmationShared(queryClient, 'step_checkpoint', true, 1);
    const second = setCheckpointConfirmationShared(queryClient, 'step_checkpoint', true, 1);
    expect(second).toBe(first);
    release(planAt(2));
    await expect(first).resolves.toEqual(planAt(2));
    expect(call).toHaveBeenCalledTimes(1);
  });

  it('does not restore cache when a confirmation response arrives after logout', async () => {
    const queryClient = client();
    let release!: (plan: Plan) => void;
    const gate = new Promise<Plan>((resolve) => { release = resolve; });
    vi.spyOn(onboardingApi, 'setCheckpointConfirmation').mockImplementation(async () => ({ status: 200, data: await gate }));

    const pending = setCheckpointConfirmationShared(queryClient, 'step_checkpoint', true, 1);
    await resetSessionState(queryClient);
    release(planAt(2));

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    expect(queryClient.getQueryData(queryKeys.plan())).toBeUndefined();
  });
});


it.each([403, 404])('does not accept a late successful PUT after access denial %s', async status => {
  const queryClient = client();
  let release!: (plan: Plan) => void;
  const gate = new Promise<Plan>(resolve => { release = resolve; });
  vi.spyOn(onboardingApi, 'setCheckpointConfirmation').mockImplementation(async () => ({ status: 200, data: await gate }));
  queryClient.setQueryData(queryKeys.plan(), planAt(1));
  const pending = setCheckpointConfirmationShared(queryClient, 'step_checkpoint', true, 1);
  handlePlanAccessError(queryClient, new ClientError('http', 'Доступ отозван', { status }));
  expect(queryClient.getQueryData(queryKeys.plan())).toBeNull();
  release(planAt(2));
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  expect(queryClient.getQueryData(queryKeys.plan())).toBeNull();
});

it('keeps newly revalidated data when an older access generation returns a higher revision', async () => {
  const queryClient = client();
  let release!: (plan: Plan) => void;
  const gate = new Promise<Plan>(resolve => { release = resolve; });
  vi.spyOn(onboardingApi, 'setCheckpointConfirmation').mockImplementation(async () => ({ status: 200, data: await gate }));
  const pending = setCheckpointConfirmationShared(queryClient, 'step_checkpoint', true, 1);
  handlePlanAccessError(queryClient, new ClientError('http', 'Доступ отозван', { status: 403 }));
  const revalidated = { ...planAt(3), title: 'Проверенный заново план' };
  acceptPlan(queryClient, revalidated, queryRequestSignal(queryClient, queryKeys.plan()));
  release(planAt(99));
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  expect(queryClient.getQueryData(queryKeys.plan())).toEqual(revalidated);
});
