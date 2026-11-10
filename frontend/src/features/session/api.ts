import { z } from 'zod';
import { apiRequest } from '@/shared/api';
import type { User } from '@/shared/api';
import { demoAccountsSchema, userSchema } from './schemas';

export const sessionApi = {
  listDemoAccounts(signal: AbortSignal): Promise<User[]> {
    return apiRequest('/demo/accounts', demoAccountsSchema, { signal })
      .then((response) => response.data ?? []);
  },

  createDemoSession(accountId: string): Promise<User> {
    return apiRequest('/demo/session', userSchema, {
      method: 'POST',
      body: { account_id: accountId },
    }).then((response) => {
      if (!response.data) {
        throw new Error('Сервер не вернул данные пользователя');
      }
      return response.data;
    });
  },

  getMe(signal: AbortSignal) {
    return apiRequest('/me', userSchema, { signal });
  },

  deleteSession() {
    return apiRequest('/session', z.undefined(), { method: 'DELETE' });
  },
};
