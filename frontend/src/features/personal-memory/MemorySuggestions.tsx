import { useEffect, useRef } from "react";
import { Link } from "react-router-dom";
import { Button, InlineError } from "@/shared/ui";
import { memoryApi, type Proposal } from "./api";
import { useMemoryAction, useMemoryProposals } from "./hooks";
import { ProposalCard } from "./ProposalCard";
import styles from "./memory.module.css";

export function MemorySuggestions({ messageId }: { messageId: string }) {
  const query = useMemoryProposals(messageId),
    action = useMemoryAction();
  const attempted = useRef("");
  const suggest = () =>
    action.run({ messageId }, (signal) => memoryApi.suggest(messageId, signal));
  useEffect(() => {
    if (
      query.data?.state === "not_requested" &&
      attempted.current !== messageId
    ) {
      attempted.current = messageId;
      void suggest();
    }
  });
  async function decide(proposal: Proposal, decision: "accept" | "dismiss") {
    await action.run(
      { id: proposal.id, decision, replaces: proposal.replaces },
      (signal) => memoryApi.decide(proposal, decision, signal),
      async (signal) => {
        const current = (
          await memoryApi.proposals(messageId, signal)
        ).items.find((p) => p.id === proposal.id);
        return current?.state ===
          (decision === "accept" ? "accepted" : "dismissed")
          ? {
              proposal: current,
              entry: current.entry_id
                ? await memoryApi.entry(current.entry_id, signal)
                : null,
            }
          : undefined;
      },
    );
  }
  if (
    query.data &&
    ["ready", "skipped"].includes(query.data.state) &&
    !query.data.items.length &&
    !action.pending &&
    !action.error
  )
    return null;
  return (
    <section
      className={styles.suggestions}
      aria-label="Предложения персональной памяти"
    >
      {query.data === null &&
        !query.isError &&
        !query.isFetching &&
        !action.pending && (
          <InlineError
            message="Предложения памяти недоступны. Проверьте доступ и повторите загрузку."
            onRetry={() => void query.refetch()}
          />
        )}
      {(query.isPending || action.pending) && (
        <p role="status">Проверяем рабочие предпочтения для памяти…</p>
      )}
      {query.isError && (
        <InlineError
          message="Не удалось загрузить предложения памяти. Чат доступен."
          onRetry={() => void query.refetch()}
        />
      )}
      {action.error && <InlineError message={action.error} />}
      {query.data?.items.length ? (
        <>
          <h2>
            {query.data.items.some((p) => p.state === "pending")
              ? "Запомнить для следующих диалогов?"
              : "Предложения памяти обработаны"}
          </h2>
          <p>Сохраняются только подтверждённые вами рабочие предпочтения.</p>
          {query.data.items.map((p) => (
            <ProposalCard
              key={p.id}
              proposal={p}
              busy={action.pending}
              onDecision={(d) => void decide(p, d)}
            />
          ))}
        </>
      ) : null}
      {(query.data?.state === "failed" ||
        (query.data?.state === "not_requested" && !!action.error)) && (
        <>
          <p>
            Не удалось подобрать факты. Память не изменена; можно продолжать
            разговор.
          </p>
          <Button
            variant="secondary"
            disabled={action.pending}
            onClick={() => void suggest()}
          >
            Повторить подбор фактов
          </Button>
        </>
      )}
      <Link to="/memory">Моя память</Link>
    </section>
  );
}
