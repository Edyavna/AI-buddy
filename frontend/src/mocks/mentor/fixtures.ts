import { createScenario } from '../fixtures/scenario';
import { questionSchema, mentorDocumentDetailSchema, internSchema, availableInternSchema } from '@/features/mentor/models';
import { planSchema } from '@/features/onboarding';

export function createMentorFixtures() {
  const base = createScenario(), date = '2026-11-11T09:00:00Z';
  const mentor = { id: 'mentor_demo_1', name: 'Тестовый наставник', role: 'mentor' as const, team: base.user.team };
  const question = questionSchema.parse({ id: 'gap_demo_1', student: base.user, step_id: 'step_local_setup', message_id: 'msg_gap_1',
    question: 'Как запустить сервис локально?', context_excerpt: 'Стажёр готовит окружение, подтверждённой инструкции пока нет.',
    status: 'new', revision: 1, document_ids: [], resolution: null, created_at: date, updated_at: date });
  const document = mentorDocumentDetailSchema.parse({ id: 'mentor_doc_1', title: 'Тестовая инструкция команды', status: 'ready', author: mentor, question_id: null,
    created_at: date, updated_at: date, error: null, content: '# Локальный запуск\n\nСинтетический пример, не регламент Авито.\n\n```sh\nnpm ci\n```' });
  const intern = internSchema.parse({ ...base.user, progress_percent: 0, next_step_id: 'step_local_setup', completed_steps: 0, total_steps: 1, blocked_steps: 0, open_questions: 1 });
  const available = availableInternSchema.parse({ id: 'intern_demo_unassigned', name: 'Свободный стажёр', team: base.user.team });
  return { mentor, internUser: { ...base.user, role: 'intern' as const }, questions: [question], documents: [document], interns: [intern], available: [available], plan: planSchema.parse(base.plan) };
}
