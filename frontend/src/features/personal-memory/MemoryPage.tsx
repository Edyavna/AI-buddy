import { useState } from "react";
import { Link } from "react-router-dom";
import { ClientError } from "@/shared/api";
import { Button, InlineError } from "@/shared/ui";
import { memoryApi, type Entry, type Option, type Proposal } from "./api";
import { useMemory, useMemoryAction } from "./hooks";
import { categoryLabel, ProposalCard } from "./ProposalCard";
import styles from "./memory.module.css";

function EntryEditor({
  entry,
  options,
  busy,
  onSave,
  onCancel,
}: {
  entry: Entry;
  options: Option[];
  busy: boolean;
  onSave: (value: string, revision: number) => void;
  onCancel: () => void;
}) {
  const [draft, setDraft] = useState({
    value: entry.value,
    revision: entry.revision,
  });
  return (
    <form
      className={styles.editor}
      onSubmit={(event) => {
        event.preventDefault();
        onSave(draft.value, draft.revision);
      }}
    >
      <label htmlFor={`memory-value-${entry.id}`}>Новое предпочтение</label>
      <select
        id={`memory-value-${entry.id}`}
        value={draft.value}
        disabled={busy}
        onChange={(event) => setDraft({ ...draft, value: event.target.value })}
      >
        {options
          .filter((o) => o.key === entry.key)
          .map((o) => (
            <option value={o.value} key={o.value}>
              {o.text}
            </option>
          ))}
      </select>
      <p>
        Изменение станет вашим подтверждённым предпочтением для следующих
        запросов.
      </p>
      <div className={styles.actions}>
        <Button type="submit" disabled={busy}>
          Сохранить предпочтение
        </Button>
        <Button variant="secondary" disabled={busy} onClick={onCancel}>
          Отмена
        </Button>
      </div>
    </form>
  );
}
export function MemoryPage() {
  const query = useMemory(),
    action = useMemoryAction();
  const [editing, setEditing] = useState<string | null>(null),
    [removing, setRemoving] = useState<string | null>(null);
  const data = query.data;
  async function edit(entry: Entry, value: string, revision: number) {
    const result = await action.run(
      { id: entry.id, value, revision },
      (signal) => memoryApi.edit(entry.id, value, revision, signal),
      async (signal) => {
        const current = await memoryApi.entry(entry.id, signal);
        return current.value === value && current.revision > revision
          ? current
          : undefined;
      },
    );
    if (result) setEditing(null);
  }
  async function remove(entry: Entry) {
    const result = await action.run(
      { id: entry.id, revision: entry.revision, remove: true },
      (signal) => memoryApi.remove(entry.id, entry.revision, signal),
      async (signal) => {
        try {
          await memoryApi.entry(entry.id, signal);
          return undefined;
        } catch (error) {
          if (
            error instanceof ClientError &&
            error.status === 404 &&
            error.code === "MEMORY_ENTRY_DELETED"
          )
            return { deleted: true as const };
          throw error;
        }
      },
    );
    if (result) setRemoving(null);
  }
  async function decide(proposal: Proposal, decision: "accept" | "dismiss") {
    await action.run(
      { id: proposal.id, decision, replaces: proposal.replaces },
      (signal) => memoryApi.decide(proposal, decision, signal),
      async (signal) => {
        const current = (
          await memoryApi.proposals(proposal.source_message_id, signal)
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
  return (
    <section className={styles.page}>
      <Link to="/chat">← К помощнику</Link>
      <h1>Моя память</h1>
      <p>
        Рабочее окружение, изучаемые технологии и удобный формат помощи. Вы
        решаете, что учитывать в следующих диалогах.
      </p>
      <Button
        variant="secondary"
        disabled={query.isFetching || action.pending}
        onClick={() => {
          setEditing(null);
          setRemoving(null);
          void query.refetch();
        }}
      >
        Перечитать память
      </Button>
      {(query.isPending || (!data && action.pending)) && (
        <p role="status">Загружаем память…</p>
      )}
      {data === null &&
        !query.isError &&
        !query.isFetching &&
        !action.pending && (
          <InlineError
            message="Память недоступна. Перечитайте данные после проверки доступа."
            onRetry={() => void query.refetch()}
          />
        )}
      {query.isError && (
        <InlineError
          message={query.error.message}
          onRetry={() => void query.refetch()}
        />
      )}
      {action.error && <InlineError message={action.error} />}
      {data && (
        <>
          <section
            className={styles.card}
            aria-label="Текущие данные приложения"
          >
            <h2>Сейчас в приложении</h2>
            <p>Команда: {data.context.team.name}</p>
            <p>Роль: стажёр</p>
            <p>Наставник: {data.context.mentor?.name ?? "Не назначен"}</p>
            <p>
              {data.context.progress_percent === null
                ? "План ещё не создан"
                : `Прогресс плана: ${data.context.progress_percent}%`}
            </p>
            <p>
              Эти данные перечитываются из приложения при каждом запросе
              помощнику.
            </p>
          </section>
          <section aria-label="Ожидают подтверждения">
            <h2>На ваше подтверждение</h2>
            {data.pending.length ? (
              data.pending.map((p) => (
                <ProposalCard
                  key={p.id}
                  proposal={p}
                  busy={action.pending}
                  onDecision={(decision) => void decide(p, decision)}
                />
              ))
            ) : (
              <p>
                Новых предложений пока нет. Сообщите помощнику, какое окружение
                используете или что изучаете.
              </p>
            )}
          </section>
          <section aria-label="Сохранённые предпочтения">
            <h2>Сохранённые предпочтения</h2>
            {data.entries.length ? (
              data.entries.map((entry) => (
                <article
                  key={entry.id}
                  className={styles.card}
                  aria-label={entry.text}
                >
                  <small>{categoryLabel[entry.category]}</small>
                  <h3>{entry.text}</h3>
                  <p>
                    {entry.edited ? "Изменено вами" : "Подтверждено вами"} ·{" "}
                    {new Date(entry.updated_at).toLocaleDateString("ru-RU")}
                  </p>
                  <a
                    href={`/chat/${encodeURIComponent(entry.conversation_id)}#message-${encodeURIComponent(entry.source_message_id)}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    Исходная реплика
                  </a>
                  {editing === entry.id ? (
                    <EntryEditor
                      entry={entry}
                      options={data.options}
                      busy={action.pending}
                      onSave={(value, revision) =>
                        void edit(entry, value, revision)
                      }
                      onCancel={() => setEditing(null)}
                    />
                  ) : (
                    <div className={styles.actions}>
                      <Button
                        variant="secondary"
                        disabled={action.pending}
                        onClick={() => {
                          setEditing(entry.id);
                          setRemoving(null);
                        }}
                      >
                        Изменить: {entry.text}
                      </Button>
                      <Button
                        variant="secondary"
                        disabled={action.pending}
                        onClick={() => {
                          setRemoving(entry.id);
                          setEditing(null);
                        }}
                      >
                        Удалить: {entry.text}
                      </Button>
                    </div>
                  )}
                  {removing === entry.id && (
                    <div role="group" aria-label="Подтверждение удаления">
                      <p>
                        Удалить это предпочтение из памяти? Со следующего
                        запроса помощник перестанет его учитывать.
                      </p>
                      <div className={styles.actions}>
                        <Button
                          variant="danger"
                          disabled={action.pending}
                          onClick={() => void remove(entry)}
                        >
                          Удалить из памяти
                        </Button>
                        <Button
                          variant="secondary"
                          disabled={action.pending}
                          onClick={() => setRemoving(null)}
                        >
                          Отмена
                        </Button>
                      </div>
                    </div>
                  )}
                </article>
              ))
            ) : (
              <p>
                Вы ещё ничего не сохранили. Предложения становятся памятью
                только после вашего подтверждения.
              </p>
            )}
          </section>
          <details className={styles.card}>
            <summary>Что учитывает помощник</summary>
            <pre>{data.profile_markdown}</pre>
          </details>
        </>
      )}
    </section>
  );
}
