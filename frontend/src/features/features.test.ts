import { describe, it, expect } from 'vitest';
import * as accessRequests from './access-requests';
import * as chat from './chat';
import * as documents from './documents';
import * as onboarding from './onboarding';
import * as session from './session';
import * as api from '@/shared/api';

describe('features and api barrels', () => {
  it('exports valid empty or defined objects', () => {
    expect(accessRequests).toBeDefined();
    expect(chat).toBeDefined();
    expect(documents).toBeDefined();
    expect(onboarding).toBeDefined();
    expect(session).toBeDefined();
    expect(api.queryKeys).toBeDefined();
  });
});
