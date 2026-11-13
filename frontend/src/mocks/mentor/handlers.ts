import { http, HttpResponse } from 'msw';
import { z } from 'zod';
import { createMentorFixtures } from './fixtures';
import { questionStatusSchema, mentorDocumentSchema } from '@/features/mentor/models';
import { setCheckpointConfirmationSchema, type Plan } from '@/features/onboarding';
import { mockSession } from '../handlers/session-bus';

export type MentorScenario = 'normal' | 'empty' | 'failed' | 'processing' | 'question_changed' | 'lost_upload' | 'assignment_conflict' | 'assignment_lost' | 'assignment_unconfirmed' | 'checkpoint_conflict' | 'checkpoint_lost' | 'checkpoint_unconfirmed';
const error = (status: number, code: string, message: string) => HttpResponse.json({ error: { code, message, trace_id: 'mentor_mock' } }, { status });

// Fully isolated session + workspace. Installed only explicitly, never a Go fallback.
export function createMentorMock(scenario: MentorScenario = 'normal', options: { plan?: Plan } = {}) {
  const f = createMentorFixtures();
  let principal: typeof f.mentor | typeof f.internUser | null = null;
  const questions = structuredClone(scenario === 'empty' ? [] : f.questions);
  const documents = structuredClone(scenario === 'empty' ? [] : f.documents);
  const interns = structuredClone(scenario === 'empty' ? [] : f.interns);
  const available = structuredClone(scenario === 'empty' ? [] : f.available);
  const plan = structuredClone(options.plan ?? f.plan);
  let checkpointConflict = false, checkpointLost = false;
  function mentorPlan(): Plan {
    return { ...plan, steps: plan.steps.map(s => ({ ...s, allowed_actions: s.completion_policy === 'intern' ? [] : s.confirmations.some(c => c.role === 'mentor' && c.user_id === f.mentor.id) ? ['revoke'] : s.status === 'blocked' ? [] : ['confirm'] })) };
  }
  function bump() { const next = Math.max(1, ...plan.steps.map(s => s.revision)) + 1; for (const s of plan.steps) s.revision = next; }
  function confirm(role: 'intern' | 'mentor', stepId: string, confirmed: boolean) {
    const s = plan.steps.find(s => s.id === stepId); if (!s || (s.completion_policy !== role && s.completion_policy !== 'both')) return;
    if (s.confirmations.some(c => c.role === role) === confirmed) return;
    s.confirmations = s.confirmations.filter(c => c.role !== role);
    if (confirmed) s.confirmations.push({ role, user_id: role === 'mentor' ? f.mentor.id : f.internUser.id, confirmed_at: '2026-11-12T12:00:00Z' });
    const own = s.confirmations.some(c => c.role === 'intern'), mentor = s.confirmations.some(c => c.role === 'mentor');
    const completed = s.completion_policy === 'both' ? own && mentor : s.completion_policy === 'intern' ? own : mentor;
    s.status = s.status === 'blocked' ? 'blocked' : completed ? 'completed' : 'in_progress';
    s.awaiting_role = s.completion_policy === 'both' && own !== mentor ? (own ? 'mentor' : 'intern') : null;
    bump();
    plan.progress_percent = plan.steps.length ? Math.floor(100 * plan.steps.filter(s => s.status === 'completed').length / plan.steps.length) : 0;
    plan.next_step_id = plan.steps.find(s => s.status !== 'completed' && s.status !== 'blocked')?.id ?? null;
  }
  let assignmentLost = false;
  const uploads = new Map<string, { signature: string; documentId: string }>();
  const reads = new Map<string, number>();
  let conflictSent = false, uploadLost = false;
  function denied() { return !principal ? error(401, 'UNAUTHENTICATED', 'Войдите в систему') : principal.role !== 'mentor' ? error(403, 'ROLE_FORBIDDEN', 'Раздел доступен наставнику') : null; }
  const missing = () => error(404, 'NOT_FOUND', 'Объект недоступен или удалён');
  function advance() {
    for (const doc of documents) {
      if (doc.status !== 'processing' || scenario === 'processing') continue;
      const count = (reads.get(doc.id) ?? 0) + 1; reads.set(doc.id, count);
      if (count < 2) continue;
      doc.status = scenario === 'failed' ? 'failed' : 'ready';
      doc.error = doc.status === 'failed' ? 'Тестовый сбой индексации' : null;
    }
  }
  const handlers = [
    http.get('*/api/v1/demo/accounts', () => HttpResponse.json([f.internUser, f.mentor])),
    http.post('*/api/v1/demo/session', async ({ request }) => {
      const input = z.object({ account_id: z.string() }).safeParse(await request.json());
      if (!input.success) return error(422, 'VALIDATION_ERROR', 'Выберите пользователя');
      principal = [f.internUser, f.mentor].find(user => user.id === input.data.account_id) ?? null;
      mockSession.set(principal);
      return principal ? HttpResponse.json(principal) : missing();
    }),
    http.get('*/api/v1/me', () => principal ? HttpResponse.json(principal) : error(401, 'UNAUTHENTICATED', 'Войдите в систему')),
    http.delete('*/api/v1/session', () => { principal = null; mockSession.set(null); return new HttpResponse(null, { status: 204 }); }),
    http.get('*/api/v1/mentor/questions', ({ request }) => {
      const denial = denied(); if (denial) return denial;
      const status = new URL(request.url).searchParams.get('status');
      return HttpResponse.json({ items: questions.filter(q => !status || q.status === status) });
    }),
    http.get('*/api/v1/mentor/questions/:id', ({ params }) => {
      const denial = denied(); if (denial) return denial;
      const q = questions.find(item => item.id === params.id); return q ? HttpResponse.json(q) : missing();
    }),
    http.patch('*/api/v1/mentor/questions/:id', async ({ params, request }) => {
      const denial = denied(); if (denial) return denial;
      const q = questions.find(item => item.id === params.id); if (!q) return missing();
      const input = z.object({ status: questionStatusSchema, revision: z.number().int().positive(), resolution: z.string().optional(), document_ids: z.array(z.string()).optional() }).strict().safeParse(await request.json());
      if (!input.success) return error(422, 'VALIDATION_ERROR', 'Проверьте поля решения');
      if (scenario === 'question_changed' && !conflictSent) { q.revision++; conflictSent = true; }
      if (input.data.revision !== q.revision) return error(409, 'QUESTION_CHANGED', 'Вопрос изменился. Проверьте новую версию.');
      const ids = input.data.document_ids ?? q.document_ids;
      if (input.data.status === 'resolved' && (!input.data.resolution?.trim() || !ids.length || !ids.every(id => documents.some(d => d.id === id && d.status === 'ready'))))
        return error(409, 'DOCUMENT_NOT_READY', 'Для решения нужны комментарий и готовый документ');
      q.status = input.data.status; q.revision++; q.document_ids = ids; q.resolution = input.data.status === 'resolved' ? input.data.resolution!.trim() : null;
      return HttpResponse.json(q);
    }),
    http.get('*/api/v1/mentor/documents', () => { const denial = denied(); if (denial) return denial; advance(); return HttpResponse.json({ items: documents.map(doc => mentorDocumentSchema.parse(doc)) }); }),
    http.get('*/api/v1/mentor/documents/:id', ({ params }) => { const denial = denied(); if (denial) return denial; advance(); const doc = documents.find(d => d.id === params.id); return doc ? HttpResponse.json(doc) : missing(); }),
    http.post('*/api/v1/mentor/documents', async ({ request }) => {
      const denial = denied(); if (denial) return denial;
      const form = await request.formData();
      const uploadId = form.get('upload_id'), title = form.get('title'), file = form.get('file'), questionId = form.get('question_id');
      if (!z.uuid().safeParse(uploadId).success || typeof title !== 'string' || !title.trim() || !file || typeof file === 'string' || form.getAll('file').length !== 1 || !/\.md$/i.test(file.name) || file.size > 1048576)
        return error(422, 'VALIDATION_ERROR', 'Нужны название и один .md файл до 1 MiB');
      if (questionId && !questions.some(q => q.id === questionId)) return missing();
      let content: string;
      try { content = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer()); } catch { return error(422, 'VALIDATION_ERROR', 'Файл должен быть UTF-8'); }
      if (!content.trim()) return error(422, 'VALIDATION_ERROR', 'Файл пуст');
      const signature = JSON.stringify([title, file.name, content, questionId]);
      const previous = uploads.get(String(uploadId));
      if (previous) return previous.signature === signature ? HttpResponse.json(documents.find(d => d.id === previous.documentId)) : error(409, 'IDEMPOTENCY_CONFLICT', 'Изменены параметры прежней загрузки');
      const doc = { ...f.documents[0]!, id: `doc_${uploadId}`, title: title.trim(), content, status: 'processing' as const, error: null,
        question_id: typeof questionId === 'string' ? questionId : null };
      documents.push(doc); uploads.set(String(uploadId), { signature, documentId: doc.id });
      const question = questions.find(q => q.id === questionId);
      if (question) { question.document_ids = [...new Set([...question.document_ids, doc.id])]; question.revision++; }
      if (scenario === 'lost_upload' && !uploadLost) { uploadLost = true; return HttpResponse.error(); }
      return HttpResponse.json(doc, { status: 202 });
    }),
    http.post('*/api/v1/mentor/documents/:id/retry', ({ params }) => {
      const denial = denied(); if (denial) return denial;
      const doc = documents.find(d => d.id === params.id); if (!doc) return missing();
      if (doc.status === 'ready') return error(409, 'DOCUMENT_ALREADY_READY', 'Документ уже готов');
      doc.status = 'processing'; doc.error = null; reads.set(doc.id, 0); return HttpResponse.json(doc, { status: 202 });
    }),
    http.get('*/api/v1/mentor/available-interns', () => { const denial = denied(); return denial ?? HttpResponse.json({ items: available }); }),
    http.put('*/api/v1/mentor/interns/:id/assignment', async ({ params, request }) => {
      const denial = denied(); if (denial) return denial;
      if ((await request.text()).length) return error(422, 'VALIDATION_ERROR', 'Тело запроса должно отсутствовать');
      const candidate = available.find(i => i.id === params.id);
      if (!candidate && !interns.some(i => i.id === params.id)) return missing();
      if (candidate && scenario === 'assignment_conflict') {
        available.splice(available.indexOf(candidate), 1);
        return error(409, 'INTERN_ALREADY_ASSIGNED', 'Стажёра уже назначили');
      }
      if (scenario === 'assignment_unconfirmed') return HttpResponse.error();
      if (candidate) {
        available.splice(available.indexOf(candidate), 1);
        interns.push({ ...candidate, progress_percent: 0, completed_steps: 0, total_steps: 0, blocked_steps: 0, open_questions: 0, next_step_id: null });
      }
      if (scenario === 'assignment_lost' && !assignmentLost) { assignmentLost = true; return HttpResponse.error(); }
      return HttpResponse.json({ intern_id: params.id, mentor_id: f.mentor.id, team_id: f.mentor.team.id });
    }),
    http.get('*/api/v1/mentor/interns', () => { const denial = denied(); if (denial) return denial; return HttpResponse.json({ items: interns.map(i => ({ ...i, ...(i.id === f.internUser.id && { progress_percent: plan.progress_percent, total_steps: plan.steps.length, completed_steps: plan.steps.filter(s => s.status === 'completed').length, blocked_steps: plan.steps.filter(s => s.status === 'blocked').length, next_step_id: plan.next_step_id }), open_questions: questions.filter(q => q.student.id === i.id && q.status !== 'resolved').length })) }); }),
    http.get('*/api/v1/mentor/interns/:id/plan', ({ params }) => { const denial = denied(); if (denial) return denial; return params.id === f.internUser.id ? HttpResponse.json(mentorPlan()) : missing(); }),
    http.put('*/api/v1/mentor/interns/:id/steps/:stepId/confirmation', async ({ params, request }) => {
      const denial = denied(); if (denial) return denial;
      if (params.id !== f.internUser.id) return missing();
      const step = plan.steps.find(s => s.id === params.stepId); if (!step) return missing();
      const body = setCheckpointConfirmationSchema.safeParse(await request.json().catch(() => null));
      if (!body.success) return error(422, 'VALIDATION_ERROR', 'Проверьте параметры');
      if (step.completion_policy === 'intern') return error(403, 'ROLE_FORBIDDEN', 'Подтверждает стажёр');
      if (scenario === 'checkpoint_unconfirmed') return HttpResponse.error();
      if (scenario === 'checkpoint_conflict' && !checkpointConflict) { checkpointConflict = true; bump(); }
      const own = step.confirmations.some(c => c.role === 'mentor' && c.user_id === f.mentor.id);
      if (own === body.data.confirmed) return HttpResponse.json(mentorPlan());
      if (step.revision !== body.data.revision) return error(409, 'CHECKPOINT_REVISION_CONFLICT', 'План изменился');
      if (step.status === 'blocked' && body.data.confirmed) return error(409, 'STEP_TRANSITION_FORBIDDEN', 'Шаг заблокирован');
      confirm('mentor', step.id, body.data.confirmed);
      if (scenario === 'checkpoint_lost' && !checkpointLost) { checkpointLost = true; return HttpResponse.error(); }
      return HttpResponse.json(mentorPlan());
    }),
  ];
  return { handlers, control: { setRole: (role: 'mentor' | 'intern' | null) => { principal = role === 'mentor' ? f.mentor : role === 'intern' ? f.internUser : null; }, questions, documents, interns, available, plan, confirmIntern: (id: string, confirmed: boolean) => confirm('intern', id, confirmed) } };
}
