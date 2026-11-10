import React, { useEffect } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { InlineError, Spinner } from '@/shared/ui';
import { isUnauthorized, useMe } from './hooks';
import { resetSessionState } from './session-state';
import styles from './RequireAuth.module.css';

// Protected-route guard (integration.md §1): load me first. 401 → login with cache
// cleanup; 503/network failure → error with retry, never a silent logout.
export function RequireAuth({ children }: { children: React.ReactNode }) {
  const me = useMe();
  const queryClient = useQueryClient();
  const location = useLocation();
  const unauthorized = me.isError && isUnauthorized(me.error);

  useEffect(() => {
    if (unauthorized) {
      void resetSessionState(queryClient);
    }
  }, [unauthorized, queryClient]);

  if (me.isPending) {
    return (
      <div className={styles.loadingState} role="status" aria-label="Проверка сессии">
        <Spinner size="lg" />
      </div>
    );
  }

  if (unauthorized) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (me.isError) {
    return (
      <InlineError
        className={styles.errorState ?? ''}
        message={me.error instanceof Error ? me.error.message : 'Не удалось проверить сессию'}
        onRetry={() => void me.refetch()}
        isRetrying={me.isRefetching}
      />
    );
  }

  return <>{children}</>;
}
