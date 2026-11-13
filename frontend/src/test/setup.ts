import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { cleanup } from '@testing-library/react';
import { createHandlers } from '../mocks/handlers';
import { mockSession } from '../mocks/handlers/session-bus';
import { server } from './server';

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  cleanup();
  mockSession.reset();
  server.resetHandlers(...createHandlers());
});
afterAll(() => server.close());
