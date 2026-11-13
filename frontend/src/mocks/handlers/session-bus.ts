import type { User } from '../../shared/api';

// Shared identity for MSW handler groups: fe2 and mentor mocks publish demo
// logins here, role-aware handlers (review preparation) read the current user.
export type MockUser = User | null;

let current: MockUser = null;
let lastIntern: MockUser = null;

export const mockSession = {
  get(): MockUser {
    return current;
  },
  lastIntern(): MockUser {
    return lastIntern;
  },
  set(user: MockUser): void {
    current = user;
    if (user?.role === 'intern') lastIntern = user;
  },
  reset(): void {
    current = null;
    lastIntern = null;
  },
};
