import { http, HttpResponse } from 'msw';
import { createScenario } from '../fixtures/scenario';
import { notFound } from './chat';
import type { MentorQuestion } from '../../features/mentor-questions';

export const KNOWLEDGE_GAP_MESSAGE_ID = 'message_gap';

export function createMentorQuestionHandlers() {
  const fixture = createScenario();
  const questions = new Map<string, MentorQuestion>();

  return [
    http.get('*/api/v1/messages/:id/knowledge-gap', ({ params }) => {
      const messageId = String(params.id);
      if (messageId !== KNOWLEDGE_GAP_MESSAGE_ID && messageId !== 'msg_a1') return notFound();

      const current = questions.get(messageId);
      if (current?.status === 'new') {
        const updated = {
          ...current,
          status: 'in_progress' as const,
          revision: current.revision + 1,
          updated_at: '2026-11-09T12:05:00Z',
        };
        questions.set(messageId, updated);
        return HttpResponse.json({ question: updated });
      }
      if (current?.status === 'in_progress') {
        const updated = {
          ...current,
          status: 'resolved' as const,
          revision: current.revision + 1,
          resolution: 'Инструкция есть в базе знаний. Откройте источник и повторите шаг по обновлённому описанию.',
          document_ids: ['doc_local_setup'],
          updated_at: '2026-11-09T12:10:00Z',
        };
        questions.set(messageId, updated);
        return HttpResponse.json({ question: updated });
      }
      return HttpResponse.json({ question: current ?? null });
    }),

    http.post('*/api/v1/messages/:id/knowledge-gap', ({ params }) => {
      const messageId = String(params.id);
      if (messageId !== KNOWLEDGE_GAP_MESSAGE_ID && messageId !== 'msg_a1') return notFound();
      const existing = questions.get(messageId);
      if (existing) return HttpResponse.json(existing);

      const question: MentorQuestion = {
        id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        message_id: messageId,
        status: 'new',
        revision: 1,
        resolution: null,
        document_ids: [],
        created_at: fixture.conversation.created_at,
        updated_at: fixture.conversation.updated_at,
      };
      questions.set(messageId, question);
      return HttpResponse.json(question, { status: 201 });
    }),
  ];
}
