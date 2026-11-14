import { useState } from "react";
import { Button } from "@/shared/ui";
import { DocumentDrawer } from "@/features/documents";
import type { ContextSnapshot as Snapshot } from "./api";
import styles from "./Context.module.css";

export function ContextSnapshot({
  snapshot,
  direction,
  questionId,
  author = false,
}: {
  snapshot: Snapshot | null | undefined;
  direction: "mentor" | "hr";
  questionId: string;
  author?: boolean;
}) {
  const [documentId, setDocumentId] = useState<string | null>(null);
  if (!snapshot) return null;
  return (
    <section
      aria-label="Отправленный снимок контекста"
      className={styles.snapshot}
    >
      <h3>Подтверждённый снимок контекста</h3>
      {snapshot.step && (
        <p>
          Чекпоинт:{" "}
          {snapshot.step.status === "available"
            ? snapshot.step.title
            : "Недоступен"}
        </p>
      )}
      <h4>Фрагменты диалога</h4>
      {snapshot.fragments.length ? (
        snapshot.fragments.map((f, i) => (
          <article key={f.message_id} id={`context-message-${f.message_id}`}>
            <a
              href={
                author
                  ? `/chat/${encodeURIComponent(snapshot.conversation_id)}#message-${encodeURIComponent(f.message_id)}`
                  : `#context-message-${encodeURIComponent(f.message_id)}`
              }
              target={author ? "_blank" : undefined}
              rel={author ? "noreferrer" : undefined}
            >
              Сообщение {i + 1} · {f.role === "user" ? "стажёр" : "помощник"}
            </a>
            <p>{f.status === "available" ? f.text : "Фрагмент недоступен"}</p>
            <small>
              {f.edited
                ? "Отредактировано и подтверждено стажёром"
                : "Цитата из исходного сообщения"}
              . Передан только выбранный фрагмент.
            </small>
          </article>
        ))
      ) : (
        <p>Фрагменты диалога не передавались.</p>
      )}
      <h4>Попытки — со слов стажёра</h4>
      <p>{snapshot.attempts || "Не указаны"}</p>
      <h4>Источники</h4>
      {snapshot.documents.length ? (
        snapshot.documents.map((d) => (
          <div key={d.document_id}>
            <p>
              {d.status === "available" ? d.title : "Источник недоступен"} ·
              версия {d.version} ·{" "}
              {d.opened
                ? "Открыт стажёром перед отправкой"
                : "Был предложен помощником; открытие не подтверждено"}
            </p>
            {d.status === "available" && (
              <Button
                variant="secondary"
                onClick={() => setDocumentId(d.document_id)}
              >
                Открыть источник контекста
              </Button>
            )}
          </div>
        ))
      ) : (
        <p>Источники не передавались.</p>
      )}
      {documentId && (
        <DocumentDrawer
          documentId={documentId}
          onClose={() => setDocumentId(null)}
          contextQuestion={{
            id: questionId,
            direction: author ? "intern" : direction,
          }}
        />
      )}
    </section>
  );
}
