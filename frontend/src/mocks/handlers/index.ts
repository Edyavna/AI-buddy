import type { RequestHandler } from 'msw';
import { createChatHandlers } from './chat';
import { createDocumentHandlers } from './documents';

// FE2 supplies its session/plan/request handlers here, through one public seam.
export function createHandlers(fe2Handlers: RequestHandler[] = []) {
  return [...fe2Handlers, ...createChatHandlers(), ...createDocumentHandlers()];
}
