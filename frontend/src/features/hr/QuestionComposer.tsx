import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, InlineError } from "@/shared/ui";
import {
  hrApi,
  directionLabel,
  questionStatus,
  type Direction,
  type Draft,
  type RoutedQuestion,
} from "./api";
import { useOperation, usePrivateQuery } from "./requests";
import styles from "./HR.module.css";
import { ContextFields } from "@/features/question-context/ContextFields";
import { ContextSnapshot } from "@/features/question-context/ContextSnapshot";
import { emptyContext, sameContext } from "@/features/question-context/api";
import contextStyles from "@/features/question-context/Context.module.css";

const directions: Direction[] = ["mentor", "hr"];

export function QuestionComposer({
  messageId,
  onOpenDocument,
  onOpenContextDocument = onOpenDocument,
}: {
  messageId: string;
  onOpenDocument: (id: string) => void;
  onOpenContextDocument?: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <section>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
      >
        {open ? "Свернуть обращения" : "Обратиться к специалисту"}
      </Button>
      {open && (
        <MessageQuestions
          key={messageId}
          messageId={messageId}
          onOpenDocument={onOpenDocument}
          onOpenContextDocument={onOpenContextDocument}
        />
      )}
    </section>
  );
}

function MessageQuestions({
  messageId,
  onOpenDocument,
  onOpenContextDocument,
}: {
  messageId: string;
  onOpenDocument: (id: string) => void;
  onOpenContextDocument: (id: string) => void;
}) {
  const key = ["hr", "message", messageId];
  const query = usePrivateQuery(
    key,
    (signal) => hrApi.message(messageId, signal),
    true,
    5000,
  );
  const [edit, setEdit] = useState<{
    draft: Draft;
    question?: RoutedQuestion;
  }>();
  const [historyId, setHistoryId] = useState("");
  const [reviewStamp, setReviewStamp] = useState<string | null>(null);
  const [contextReady, setContextReady] = useState(true);
  const history = usePrivateQuery(
    ["hr", "history", historyId],
    (signal) => hrApi.history(historyId, signal),
    Boolean(historyId),
  );
  const client = useQueryClient();
  const op = useOperation(key);

  if (!query.data)
    return query.isError ? (
      <InlineError
        message={query.error.message}
        onRetry={() => void query.refetch()}
      />
    ) : (
      <p role="status">Загрузка обращений…</p>
    );

  const data = query.data;
  const suggested = data.routing?.direction;
  const remaining = directions.filter(
    (direction) => !data.items.some((q) => q.original_direction === direction),
  );
  const defaultDirection =
    data.drafts.find((d) => remaining.includes(d.direction))?.direction ??
    (suggested &&
    suggested !== "needs_clarification" &&
    remaining.includes(suggested)
      ? suggested
      : remaining[0]);
  const current: typeof edit =
    edit ??
    (defaultDirection
      ? {
          draft: data.drafts.find((d) => d.direction === defaultDirection) ?? {
            direction: defaultDirection,
            question:
              data.items.length && data.routing?.split ? "" : data.question,
            context_excerpt: "",
          },
        }
      : undefined);
  const currentStamp = JSON.stringify(current ?? null);
  const reviewed = reviewStamp === currentStamp;
  function setReviewed(value: boolean) {
    setReviewStamp(value ? currentStamp : null);
  }

  function update(draft: Draft) {
    setReviewed(false);
    setEdit({ ...current, draft });
  }

  async function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!current || !reviewed || !contextReady) return;
    const { draft, question } = current;
    const approved = {
      ...draft,
      context_snapshot: {
        ...(draft.context_snapshot ?? emptyContext()),
        reviewed: true,
      },
    };
    const saved = await op.run(
      { draft: approved, question: question?.id, revision: question?.revision },
      async (operation_id, signal) => {
        await hrApi.draft(
          messageId,
          {
            ...draft,
            context_snapshot: { ...approved.context_snapshot, reviewed: false },
          },
          signal,
        );
        return question
          ? hrApi.reroute(
              question.id,
              { ...approved, operation_id, revision: question.revision },
              signal,
            )
          : hrApi.send(messageId, { ...approved, operation_id }, signal);
      },
      async (signal) =>
        (await hrApi.message(messageId, signal)).items.find(
          (q) =>
            (question
              ? q.id === question.id && q.revision > question.revision
              : q.original_direction === draft.direction) &&
            q.direction === draft.direction &&
            q.question === draft.question &&
            q.context_excerpt === draft.context_excerpt &&
            sameContext(q.context_snapshot, approved.context_snapshot),
        ),
    );
    if (!saved) {
      await client.invalidateQueries({ queryKey: key });
      return;
    }
    setEdit(undefined);
    setReviewed(false);
    await client.invalidateQueries({ queryKey: key });
    if (question)
      await client.invalidateQueries({
        queryKey: ["hr", "history", question.id],
      });
  }

  async function saveDraft(draft: Draft) {
    const saved = await op.run(draft, (_, signal) =>
      hrApi.draft(messageId, draft, signal),
    );
    if (saved) await client.invalidateQueries({ queryKey: key });
  }

  return (
    <div className={styles.page}>
      {data.routing ? (
        <p>
          {suggested === "needs_clarification"
            ? data.routing.clarification
            : `Предложенный адресат — ${directionLabel[suggested ?? "mentor"]}. ${data.routing.reason}`}
          {data.routing.split &&
            " Можно отправить две части по отдельности, отредактировав текст каждой."}
        </p>
      ) : (
        <p>Выберите, кому отправить вопрос.</p>
      )}
      {data.items.map((q) => (
        <div className={styles.card} key={q.id}>
          <strong>
            {directionLabel[q.direction]} · {questionStatus[q.status]}
          </strong>
          <p>{q.question}</p>
          {q.context_excerpt && (
            <p>Резюме, подтверждённое вами: {q.context_excerpt}</p>
          )}
          <ContextSnapshot
            snapshot={q.context_snapshot}
            questionId={q.id}
            direction={q.direction}
            author
          />
          {q.resolution && <p>{q.resolution}</p>}
          {q.document_ids.map((id) => (
            <Button
              key={id}
              size="sm"
              variant="secondary"
              onClick={() => onOpenDocument(id)}
            >
              Открыть инструкцию
            </Button>
          ))}
          <div className={styles.actions}>
            <Button
              variant="secondary"
              size="sm"
              disabled={op.pending}
              onClick={() => {
                setEdit({
                  question: q,
                  draft: {
                    direction: q.direction === "hr" ? "mentor" : "hr",
                    question: q.question,
                    context_excerpt: "",
                  },
                });
                setReviewed(false);
              }}
            >
              Изменить адресата
            </Button>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setHistoryId(historyId === q.id ? "" : q.id)}
            >
              История
            </Button>
          </div>
          {historyId === q.id &&
            (history.isError ? (
              <InlineError
                message={history.error.message}
                onRetry={() => void history.refetch()}
              />
            ) : (
              <ol>
                {history.data?.history.map((entry) => (
                  <li key={entry.revision}>
                    {entry.snapshot.direction === "hr" ? "HR" : "Наставник"}
                    {typeof entry.snapshot.resolution === "string" && (
                      <p>{entry.snapshot.resolution}</p>
                    )}
                  </li>
                ))}
              </ol>
            ))}
        </div>
      ))}
      {current && (
        <form onSubmit={(event) => void send(event)} className={styles.form}>
          <label>
            Адресат
            <select
              value={current.draft.direction}
              disabled={op.pending}
              onChange={(event) => {
                const direction = event.target.value as Direction;
                update(
                  !current.question
                    ? (data.drafts.find((d) => d.direction === direction) ?? {
                        direction,
                        question: current.draft.question,
                        context_excerpt: "",
                        context_snapshot: emptyContext(),
                      })
                    : {
                        direction,
                        question: current.draft.question,
                        context_excerpt: "",
                        context_snapshot: emptyContext(),
                      },
                );
              }}
            >
              {(current.question
                ? directions.filter((d) => d !== current.question?.direction)
                : remaining
              ).map((direction) => (
                <option value={direction} key={direction}>
                  {directionLabel[direction]}
                </option>
              ))}
            </select>
          </label>
          <label>
            Вопрос
            <textarea
              required
              rows={3}
              maxLength={8000}
              disabled={op.pending}
              value={current.draft.question}
              onChange={(event) =>
                update({ ...current.draft, question: event.target.value })
              }
            />
          </label>
          <label>
            Контекст для специалиста
            <textarea
              rows={3}
              maxLength={8000}
              disabled={op.pending}
              value={current.draft.context_excerpt}
              onChange={(event) =>
                update({
                  ...current.draft,
                  context_excerpt: event.target.value,
                })
              }
            />
          </label>
          <p>
            Это ваше резюме. Предпринятые попытки и выбранные цитаты будут
            показаны отдельно.
          </p>
          <ContextFields
            key={`${messageId}:${current.draft.direction}:${current.question?.id ?? ""}`}
            messageId={messageId}
            direction={current.draft.direction}
            value={current.draft.context_snapshot ?? emptyContext()}
            onChange={(context_snapshot) =>
              update({ ...current.draft, context_snapshot })
            }
            onReady={setContextReady}
            onOpenDocument={onOpenContextDocument}
            disabled={op.pending}
          />
          <label className={contextStyles.review}>
            <span>
              <input
                type="checkbox"
                required
                checked={reviewed}
                disabled={op.pending || !contextReady}
                onChange={(e) => setReviewed(e.target.checked)}
              />{" "}
              Я проверил вопрос, адресата и весь отправляемый контекст
            </span>
          </label>
          {!data.available_directions.includes(current.draft.direction) && (
            <p role="status">
              {current.draft.direction === "hr"
                ? "Для вашей команды пока не назначен HR."
                : "Вам пока не назначен наставник."}
            </p>
          )}
          <div className={styles.actions}>
            <Button
              type="submit"
              isLoading={op.pending}
              disabled={
                !reviewed ||
                !contextReady ||
                !data.available_directions.includes(current.draft.direction)
              }
            >
              {current.question
                ? "Подтвердить перенаправление"
                : "Подтвердить отправку"}
            </Button>
            {!current.question && (
              <Button
                type="button"
                variant="secondary"
                disabled={op.pending || !current.draft.question.trim()}
                onClick={() => void saveDraft(current.draft)}
              >
                Сохранить черновик
              </Button>
            )}
            {current.question && (
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  setEdit(undefined);
                  setReviewed(false);
                }}
              >
                Отмена
              </Button>
            )}
          </div>
        </form>
      )}
      {op.error && <InlineError message={op.error} />}
    </div>
  );
}
