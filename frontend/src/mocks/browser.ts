import type { RequestHandler } from 'msw';
import { createHandlers } from './handlers';

// FE2 awaits this before mounting React ONLY in an explicitly opted-in dev build.
// Production returns before even importing the service-worker runtime.
export async function startBrowserMocks(options: {
  enabled: boolean; development: boolean; handlers?: RequestHandler[];
}) {
  if (!options.development || !options.enabled) return undefined;
  const { setupWorker } = await import('msw/browser');
  const worker = setupWorker(...createHandlers(options.handlers));
  await worker.start({ onUnhandledRequest: 'error' });
  return worker;
}
