import type { RequestHandler } from 'msw';
import { createHandlers } from './handlers';

// FE2 awaits this before mounting React ONLY in an explicitly opted-in dev build.
// Production returns before even importing the service-worker runtime.
export async function startBrowserMocks(options: {
  enabled: boolean; development: boolean; handlers?: RequestHandler[];
  mentor?: boolean;
}) {
  if (!options.development || !options.enabled) return undefined;
  const { setupWorker } = await import('msw/browser');
  const mentor = options.mentor ? (await import('./mentor/handlers')).createMentorMock().handlers : [];
  const worker = setupWorker(...mentor, ...createHandlers(options.handlers));
  await worker.start({ onUnhandledRequest: 'error' });
  return worker;
}
