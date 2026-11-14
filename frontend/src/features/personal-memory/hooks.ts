import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { denied, useOperation, usePrivateQuery } from "@/features/hr/requests";
import {
  queryAccessSignal,
  revokeQueryAccess,
} from "@/shared/api/query-access";
import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { memoryApi, memoryKeys } from "./api";

async function eraseMemory(client: QueryClient) {
  revokeQueryAccess(client, memoryKeys.all);
  for (const query of client
    .getQueryCache()
    .findAll({ queryKey: memoryKeys.all })) {
    revokeQueryAccess(client, query.queryKey);
    await client.cancelQueries(
      { queryKey: query.queryKey, exact: true },
      { revert: false },
    );
    client.setQueryData(query.queryKey, null);
  }
}
function useMemoryRead<T>(
  key: QueryKey,
  load: (signal: AbortSignal) => Promise<T>,
) {
  const client = useQueryClient();
  return usePrivateQuery(key, async (signal) => {
    const scope = AbortSignal.any([
      signal,
      queryAccessSignal(client, memoryKeys.all),
    ]);
    try {
      const data = await load(scope);
      scope.throwIfAborted();
      return data;
    } catch (error) {
      if (!scope.aborted && denied(error)) await eraseMemory(client);
      throw error;
    }
  });
}
export const useMemory = () =>
  useMemoryRead(memoryKeys.workspace, memoryApi.workspace);
export const useMemoryProposals = (id: string) =>
  useMemoryRead(memoryKeys.message(id), (signal) =>
    memoryApi.proposals(id, signal),
  );
export function useMemoryAction() {
  const client = useQueryClient();
  const action = useOperation(memoryKeys.all);
  const active = useRef(false);
  const [refreshing, setRefreshing] = useState(false);
  async function run<T>(
    payload: unknown,
    send: (signal: AbortSignal) => Promise<T>,
    reconcile?: (signal: AbortSignal) => Promise<T | undefined>,
  ) {
    if (active.current) return;
    active.current = true;
    setRefreshing(true);
    try {
      async function protect(
        work: (signal: AbortSignal) => Promise<T | undefined>,
        signal: AbortSignal,
      ) {
        try {
          return await work(signal);
        } catch (error) {
          if (!signal.aborted && denied(error)) await eraseMemory(client);
          throw error;
        }
      }
      const result = await action.run(
        payload,
        (_, signal) => protect(send, signal),
        reconcile ? (signal) => protect(reconcile, signal) : undefined,
      );
      if (result !== undefined) {
        // Mutations invalidate old read generations as well as their cache values.
        await eraseMemory(client);
        await client.invalidateQueries({ queryKey: memoryKeys.all });
      }
      return result;
    } finally {
      active.current = false;
      setRefreshing(false);
    }
  }
  return { run, pending: action.pending || refreshing, error: action.error };
}
