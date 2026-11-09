import { http, HttpResponse } from 'msw';
import type { Document } from '../../shared/api';
import { createScenario } from '../fixtures/scenario';
import { notFound } from './chat';

export function createDocumentHandlers() {
  const f = createScenario();
  const source = f.messages.flatMap(message => message.sources)[0];
  if (!source) throw new Error('Canonical fixture must contain a document source');
  const document: Document = {
    id: source.document_id, title: source.title,
    content: `# ${source.title}\n\nСинтетическая инструкция для тестирования. Не является регламентом Авито.\n\n${source.excerpt}`,
    updated_at: f.conversation.updated_at,
  };
  return [http.get('*/api/v1/documents/:id', ({ params }) =>
    params.id === document.id ? HttpResponse.json(document) : notFound())];
}
