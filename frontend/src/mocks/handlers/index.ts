import type { RequestHandler } from 'msw';
import { createChatHandlers } from './chat';
import { createDocumentHandlers } from './documents';
import { createFe2Handlers } from './fe2';

// FE2 supplies its session/plan/request handlers through one public seam.
export function createHandlers(fe2Handlers: RequestHandler[] = createFe2Handlers()) {
  return [...fe2Handlers, ...createChatHandlers(), ...createDocumentHandlers()];
}
