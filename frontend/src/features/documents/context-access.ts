import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { revokeQueryAccess } from "@/shared/api/query-access";

// A document denial also invalidates copies in previews and sent snapshots.
// Otherwise a following 503 could leave the denied source's title/quote visible
// in the parent while only the drawer itself had been cleared.
export function revokeDocumentContext(
  client: QueryClient,
  sourceMessageId: string | undefined,
  question: { id: string; direction: "mentor" | "hr" | "intern" } | undefined,
) {
  const prefixes: QueryKey[] = sourceMessageId
    ? [
        ["question-context", sourceMessageId],
        ["hr", "message", sourceMessageId],
      ]
    : question
      ? [
          [
            question.direction === "mentor" ? "mentor" : "hr",
            "question",
            question.id,
          ],
          ["hr", "history", question.id],
          ["hr", "message"],
          ["question-context"],
        ]
      : [];
  for (const prefix of prefixes) {
    for (const query of client.getQueryCache().findAll({ queryKey: prefix })) {
      revokeQueryAccess(client, query.queryKey);
      void client.cancelQueries(
        { queryKey: query.queryKey, exact: true },
        { revert: false },
      );
      client.setQueryData(query.queryKey, null);
    }
    void client.invalidateQueries({ queryKey: prefix });
  }
}
