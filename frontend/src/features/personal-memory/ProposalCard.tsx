import { Button } from "@/shared/ui";
import type { Proposal } from "./api";
import styles from "./memory.module.css";

export const categoryLabel = {
  work_environment: "Рабочее окружение",
  learning: "Изучаемые технологии",
  instruction_format: "Формат инструкций",
};
export function ProposalCard({
  proposal,
  busy,
  onDecision,
}: {
  proposal: Proposal;
  busy: boolean;
  onDecision: (decision: "accept" | "dismiss") => void;
}) {
  return (
    <article className={styles.card} aria-label={proposal.text}>
      <small>{categoryLabel[proposal.category]}</small>
      <h3>{proposal.text}</h3>
      <a
        href={`/chat/${encodeURIComponent(proposal.conversation_id)}#message-${encodeURIComponent(proposal.source_message_id)}`}
        target="_blank"
        rel="noreferrer"
      >
        Исходная реплика
      </a>
      {proposal.state === "pending" ? (
        <>
          {proposal.replaces && (
            <p>Заменит сохранённое предпочтение: {proposal.replaces.text}</p>
          )}
          <div className={styles.actions}>
            <Button disabled={busy} onClick={() => onDecision("accept")}>
              Запомнить: {proposal.text}
            </Button>
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => onDecision("dismiss")}
            >
              Не сохранять
            </Button>
          </div>
        </>
      ) : (
        <p role="status">
          {proposal.state === "accepted"
            ? "Предложение подтверждено. Актуальные записи — в разделе «Моя память»."
            : "Предложение отклонено."}
        </p>
      )}
    </article>
  );
}
