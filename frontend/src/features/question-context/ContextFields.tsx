import { useEffect } from "react";
import { Button, InlineError } from "@/shared/ui";
import { denied, usePrivateQuery } from "@/features/hr/requests";
import { contextKey, previewContext, type ContextInput } from "./api";
import styles from "./Context.module.css";

export function ContextFields({
  messageId,
  direction,
  value,
  onChange,
  onReady,
  onOpenDocument,
  disabled,
}: {
  messageId: string;
  direction: "mentor" | "hr";
  value: ContextInput;
  onChange: (value: ContextInput) => void;
  onReady: (ready: boolean) => void;
  onOpenDocument: (id: string) => void;
  disabled: boolean;
}) {
  const query = usePrivateQuery(contextKey(messageId, direction), (signal) =>
    previewContext(messageId, direction, signal),
  );
  const data = query.data;
  const missingFragment = value.fragments.some(
    (f) =>
      !data?.fragments.some(
        (item) =>
          item.message_id === f.message_id && item.status === "available",
      ),
  );
  const missingDocument = value.documents.some(
    (d) =>
      !data?.documents.some(
        (item) =>
          item.document_id === d.document_id &&
          item.version === d.version &&
          item.status === "available",
      ),
  );
  const blocked =
    data === null ||
    denied(query.error) ||
    missingFragment ||
    missingDocument ||
    (value.include_step &&
      (data?.step?.status !== "available" ||
        data.step.revision !== value.step_revision));
  useEffect(() => onReady(!blocked), [blocked, onReady]);
  const update = (next: Partial<ContextInput>) =>
    onChange({ ...value, ...next, reviewed: false });
  const missing = value.fragments.filter(
    (f) =>
      !data?.fragments.some(
        (item) =>
          item.message_id === f.message_id && item.status === "available",
      ),
  );
  const missingDocs = value.documents.filter(
    (d) =>
      !data?.documents.some(
        (item) =>
          item.document_id === d.document_id &&
          item.version === d.version &&
          item.status === "available",
      ),
  );
  return (
    <section className={styles.fields} aria-label="Контекст перед отправкой">
      <h3>Подтверждённые данные и выбранные фрагменты</h3>
      <p>
        Цитатный черновик из реального диалога. Выберите только то, что должен
        получить адресат. Весь чат не передаётся.
      </p>
      {query.isPending && <p role="status">Собираем контекст…</p>}
      {query.isError && (
        <InlineError
          message={
            denied(query.error)
              ? "Контекст недоступен. Проверьте доступ к диалогу."
              : "Не удалось собрать контекст. Можно заполнить резюме и попытки вручную без ссылок."
          }
          onRetry={() => void query.refetch()}
        />
      )}
      <Button
        variant="secondary"
        disabled={disabled || query.isFetching}
        onClick={() => void query.refetch()}
      >
        Обновить доступные фрагменты
      </Button>
      {(data?.step || value.include_step) && (
        <label>
          <span>
            <input
              type="checkbox"
              checked={value.include_step}
              disabled={disabled}
              onChange={(e) =>
                update({
                  include_step: e.target.checked,
                  step_revision: e.target.checked
                    ? (data?.step?.revision ?? null)
                    : null,
                })
              }
            />{" "}
            Включить чекпоинт:{" "}
            {data?.step?.status === "available"
              ? data.step.title
              : "Чекпоинт недоступен"}
          </span>
        </label>
      )}
      {value.include_step &&
        data?.step?.status === "available" &&
        data.step.revision !== value.step_revision && (
          <p role="status">
            Чекпоинт изменился. Снимите отметку и выберите актуальную версию
            заново.
          </p>
        )}
      {data && !data.step && <p>Вопрос без чекпоинта.</p>}
      <fieldset>
        <legend>Резюме диалога — выберите и проверьте фрагменты</legend>
        {data?.fragments
          .filter((f) => f.status === "available")
          .map((f, i) => {
            const selected = value.fragments.find(
              (item) => item.message_id === f.message_id,
            );
            return (
              <div key={f.message_id}>
                <label>
                  <span>
                    <input
                      type="checkbox"
                      checked={!!selected}
                      disabled={disabled || f.status !== "available"}
                      onChange={(e) =>
                        update({
                          fragments: e.target.checked
                            ? [
                                ...value.fragments,
                                {
                                  message_id: f.message_id,
                                  text: f.text ?? "",
                                },
                              ]
                            : value.fragments.filter(
                                (item) => item.message_id !== f.message_id,
                              ),
                        })
                      }
                    />{" "}
                    Включить сообщение {i + 1}:{" "}
                    {f.role === "user" ? "стажёр" : "помощник"}
                  </span>
                </label>
                {selected ? (
                  <label>
                    Резюме сообщения {i + 1}
                    <textarea
                      rows={3}
                      maxLength={800}
                      disabled={disabled}
                      value={selected.text}
                      onChange={(e) =>
                        update({
                          fragments: value.fragments.map((item) =>
                            item.message_id === f.message_id
                              ? { ...item, text: e.target.value }
                              : item,
                          ),
                        })
                      }
                    />
                  </label>
                ) : (
                  <p>{f.text}</p>
                )}
                <a
                  href={`/chat/${encodeURIComponent(data.conversation_id)}#message-${encodeURIComponent(f.message_id)}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  Исходное сообщение {i + 1}
                </a>
              </div>
            );
          })}
        {missing.map((f) => (
          <label key={f.message_id}>
            <span>
              <input
                type="checkbox"
                checked
                disabled={disabled}
                onChange={() =>
                  update({
                    fragments: value.fragments.filter(
                      (item) => item.message_id !== f.message_id,
                    ),
                  })
                }
              />{" "}
              Удалить недоступный фрагмент
            </span>
          </label>
        ))}
        {data && !data.fragments.some((f) => f.status === "available") && (
          <p>Подходящих сообщений нет. Заполните контекст вручную.</p>
        )}
      </fieldset>
      <fieldset>
        <legend>Источники для специалиста</legend>
        {data?.documents
          .filter((d) => d.status === "available")
          .map((d) => (
            <div key={d.document_id}>
              <label>
                <span>
                  <input
                    type="checkbox"
                    checked={value.documents.some(
                      (item) =>
                        item.document_id === d.document_id &&
                        item.version === d.version,
                    )}
                    disabled={disabled}
                    onChange={(e) =>
                      update({
                        documents: e.target.checked
                          ? [
                              ...value.documents.filter(
                                (item) => item.document_id !== d.document_id,
                              ),
                              {
                                document_id: d.document_id,
                                version: d.version,
                              },
                            ]
                          : value.documents.filter(
                              (item) =>
                                item.document_id !== d.document_id ||
                                item.version !== d.version,
                            ),
                      })
                    }
                  />{" "}
                  {d.title} · версия {d.version} ·{" "}
                  {d.opened ? "Открыта вами" : "Предложена помощником"}
                </span>
              </label>
              <Button
                variant="secondary"
                disabled={disabled}
                onClick={() => onOpenDocument(d.document_id)}
              >
                Открыть источник: {d.title}
              </Button>
            </div>
          ))}
        {missingDocs.map((d) => (
          <label key={d.document_id}>
            <span>
              <input
                type="checkbox"
                checked
                disabled={disabled}
                onChange={() =>
                  update({
                    documents: value.documents.filter(
                      (item) =>
                        item.document_id !== d.document_id ||
                        item.version !== d.version,
                    ),
                  })
                }
              />{" "}
              Удалить недоступную версию источника
            </span>
          </label>
        ))}
        {data && !data.documents.some((d) => d.status === "available") && (
          <p>Доступных источников нет.</p>
        )}
      </fieldset>
      <label>
        Что уже пробовал — со слов стажёра
        <textarea
          rows={3}
          maxLength={4000}
          value={value.attempts}
          disabled={disabled}
          onChange={(e) => update({ attempts: e.target.value })}
        />
      </label>
      <section aria-label="Предпросмотр отправляемого контекста">
        <h3>Специалист получит только этот снимок</h3>
        <p>
          Чекпоинт:{" "}
          {value.include_step
            ? data?.step?.status === "available" &&
              data.step.revision === value.step_revision
              ? data.step.title
              : "Недоступен или изменился"
            : "Не передаётся"}
        </p>
        {value.fragments.map((f) => (
          <p key={f.message_id}>
            {data?.fragments.some(
              (item) =>
                item.message_id === f.message_id && item.status === "available",
            )
              ? f.text
              : "Фрагмент недоступен — исключите его"}
          </p>
        ))}
        {value.documents.map((d) => (
          <p key={d.document_id}>
            {data?.documents.find(
              (item) =>
                item.document_id === d.document_id &&
                item.version === d.version &&
                item.status === "available",
            )?.title ?? "Источник недоступен — исключите его"}{" "}
            · версия {d.version}
          </p>
        ))}
        <p>Попытки (описание стажёра): {value.attempts || "Не указаны"}</p>
      </section>
      {blocked && (
        <p role="status">
          Исключите недоступные элементы или восстановите доступ перед
          подтверждением.
        </p>
      )}
      {data?.limited && (
        <p>
          Показаны до 12 последних сообщений и до 10 источников. Можно добавить
          описание вручную.
        </p>
      )}
    </section>
  );
}
