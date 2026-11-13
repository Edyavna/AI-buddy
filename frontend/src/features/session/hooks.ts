import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { ClientError, queryKeys } from '@/shared/api';
import type { User } from '@/shared/api';
import { sessionApi } from './api';
import { roleHome } from './roles';
import { resetSessionState } from './session-state';

function requireUser(data: User | undefined): User {
  if (!data) throw new ClientError('protocol', 'Сервер вернул пустой ответ');
  return data;
}

export function useMe() {
  return useQuery({
    queryKey: queryKeys.me(),
    queryFn: ({ signal }) => sessionApi.getMe(signal).then((r) => requireUser(r.data)),
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}

export function useDemoAccounts() {
  return useQuery({
    queryKey: ['session', 'demo-accounts'],
    queryFn: ({ signal }) => sessionApi.listDemoAccounts(signal),
    staleTime: Infinity,
    retry: false,
  });
}

export function useLogin() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    mutationFn: (accountId: string) => sessionApi.createDemoSession(accountId),
    onSuccess: async (user) => {
      await resetSessionState(queryClient);
      queryClient.setQueryData(queryKeys.me(), user);
      navigate(roleHome[user.role], { replace: true });
    },
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    mutationFn: () => sessionApi.deleteSession(),
    // Local cleanup happens even if the server call failed: the user asked to leave.
    onSettled: async () => {
      await resetSessionState(queryClient);
      navigate('/login', { replace: true });
    },
  });
}

export function isUnauthorized(error: unknown): boolean {
  return error instanceof ClientError && error.status === 401;
}
