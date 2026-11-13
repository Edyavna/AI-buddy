import { useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { ClientError } from "@/shared/api";
import {
  queryRequestSignal,
  revokeQueryAccess,
} from "@/shared/api/query-access";
import { Button, EmptyState, InlineError, TextField } from "@/shared/ui";
import { MarkdownContent } from "@/features/documents";
import {
  useAction,
  useMentorQuery,
  QueryFeedback,
  accessDenied,
} from "@/features/mentor/common";
import { useAccessScope, useMe } from "@/features/session";
import type { Direction } from "@/features/resolution-drafts/api";
import {
  groupApi,
  groupKeys,
  type Group,
  type GroupInput,
  type GroupWorkspace,
} from "./api";
import styles from "./QuestionGroups.module.css";
import {
  chooseGroupEdit,
  editFields,
  groupEdit,
  refreshGroupEdit,
  type EditField,
} from "./editor-state";

function useGroupAction(key: QueryKey) {
  const client = useQueryClient(),
    action = useAction();
  function run<T>(
    send: (signal: AbortSignal) => Promise<T>,
    verify: (signal: AbortSignal) => Promise<T | undefined>,
    accept: (result: T) => void,
  ) {
    void action.run(async (outer) => {
      const scope = AbortSignal.any([outer, queryRequestSignal(client, key)]);
      const timed = () => AbortSignal.any([scope, AbortSignal.timeout(10000)]);
      try {
        let result: T;
        try {
          result = await send(timed());
        } catch (error) {
          scope.throwIfAborted();
          if (
            !(error instanceof ClientError) ||
            accessDenied(error) ||
            !(
              ["network", "protocol", "aborted"].includes(error.kind) ||
              (error.status ?? 0) >= 500
            )
          )
            throw error;
          const latest = await verify(timed());
          scope.throwIfAborted();
          if (!latest) throw error;
          result = latest;
        }
        scope.throwIfAborted();
        accept(result);
      } catch (error) {
        if (!scope.aborted && accessDenied(error)) {
          revokeQueryAccess(client, key);
          await client.cancelQueries({ queryKey: key }, { revert: false });
          client.setQueryData(key, null);
        }
        throw error;
      }
    });
  }
  return { ...action, run };
}

export function QuestionGroupsPage({ direction }: { direction: Direction }) {
  const query = useMentorQuery(groupKeys.workspace(direction), (signal) =>
    groupApi.workspace(direction, signal),
  );
  return (
    <section className={styles.page}>
      <Link to={`/${direction}/questions`}>← К вопросам</Link>
      <header>
        <h1>Один ответ на похожие вопросы</h1>
        <p>
          Проверьте предложения или выберите состав вручную. Участники не увидят
          чужие обращения.
        </p>
        {direction === "hr" && (
          <p>
            Доступны открытые обращения, которые вы взяли в работу, в пределах
            одной команды.
          </p>
        )}
      </header>
      <QueryFeedback query={query} />
      {!query.data && !query.isPending && !query.isError && (
        <InlineError
          message="Группа недоступна или доступ изменился"
          onRetry={() => void query.refetch()}
        />
      )}{" "}
      {query.data && (
        <Workspace key={direction} direction={direction} data={query.data} />
      )}
    </section>
  );
}
function Workspace({
  direction,
  data,
}: {
  direction: Direction;
  data: GroupWorkspace;
}) {
  const navigate = useNavigate(),
    op = useGroupAction(groupKeys.workspace(direction));
  const [selected, setSelected] = useState<string[]>([]),
    [title, setTitle] = useState(""),
    [resolution, setResolution] = useState("");
  const attempt = useRef<GroupInput | null>(null);
  const [locked, setLocked] = useState(false);
  function create() {
    const body = attempt.current ?? {
      operation_id: crypto.randomUUID(),
      revision: 0,
      title,
      resolution,
      question_ids: selected,
    };
    attempt.current = body;
    setLocked(true);
    op.run(
      (signal) => groupApi.save(direction, "", body, signal),
      async (signal) => {
        try {
          return await groupApi.detail(direction, body.operation_id, signal);
        } catch (error) {
          if (error instanceof ClientError && error.status === 404)
            return undefined;
          throw error;
        }
      },
      (g) =>
        navigate(`/${direction}/question-groups/${encodeURIComponent(g.id)}`),
    );
  }
  return (
    <>
      <section className={styles.card}>
        <h2>Предложения для проверки</h2>
        {data.suggestions.length ? (
          data.suggestions.map((s, i) => (
            <div key={i}>
              <p>{s.reason}</p>
              <Button
                variant="secondary"
                disabled={locked || op.busy}
                onClick={() => setSelected(s.question_ids)}
              >
                Выбрать предложение {i + 1}
              </Button>
            </div>
          ))
        ) : (
          <p>
            Убедительных совпадений нет. Можно создать группу вручную, даже из
            одного вопроса.
          </p>
        )}
      </section>
      <form
        className={styles.card}
        onSubmit={(e) => {
          e.preventDefault();
          create();
        }}
      >
        <h2>Подтвердить состав группы</h2>
        <p>
          В одной группе — вопросы одного направления и одной команды, до 30
          участников. Уже связанные вопросы не объединяются автоматически.
        </p>
        <TextField
          label="Название темы"
          value={title}
          maxLength={200}
          required
          disabled={locked || op.busy}
          onChange={(e) => setTitle(e.target.value)}
        />
        <label className={styles.field}>
          Общее решение без личных подробностей
          <textarea
            maxLength={8000}
            value={resolution}
            disabled={locked || op.busy}
            onChange={(e) => setResolution(e.target.value)}
          />
        </label>
        <fieldset>
          <legend>Открытые вопросы</legend>
          {data.candidates.length ? (
            data.candidates.map((q) => (
              <label key={q.id} className={styles.check}>
                <input
                  type="checkbox"
                  checked={selected.includes(q.id)}
                  disabled={locked || op.busy}
                  onChange={(e) =>
                    setSelected((current) =>
                      e.target.checked
                        ? [...current, q.id]
                        : current.filter((id) => id !== q.id),
                    )
                  }
                />
                <span>
                  {q.question}
                  <small>
                    {q.student_name} ·{" "}
                    {q.step_id ? "Есть чекпоинт" : "Без чекпоинта"}
                  </small>
                </span>
              </label>
            ))
          ) : (
            <p>Подходящих открытых вопросов пока нет.</p>
          )}
        </fieldset>
        {locked && (
          <p role="status">
            Повтор использует тот же состав и идентификатор. Успех
            подтверждается сервером.
          </p>
        )}
        <div className={styles.row}>
          <Button
            type="submit"
            isLoading={op.busy}
            disabled={!selected.length || selected.length > 30 || !title.trim()}
          >
            {locked ? "Повторить создание группы" : "Подтвердить группу"}
          </Button>
          {locked && (
            <Button
              variant="secondary"
              disabled={op.busy}
              onClick={() => {
                attempt.current = null;
                setLocked(false);
              }}
            >
              Изменить состав
            </Button>
          )}
        </div>
      </form>
      {op.error && <InlineError message={op.error} />}
      {data.limited && <p>Показаны последние 100 доступных вопросов.</p>}
      <section className={styles.card}>
        <h2>Мои группы</h2>
        {data.groups.length ? (
          <ul>
            {data.groups.map((g) => (
              <li key={g.id}>
                <Link
                  to={`/${direction}/question-groups/${encodeURIComponent(g.id)}`}
                >
                  {g.title}
                </Link>
                <p>
                  {g.members.length} вопросов · доставлено {g.delivered_count}
                  {g.published_at
                    ? ` · повторных вопросов: ${g.repeat_count_limited ? "не менее " : ""}${g.repeat_count}`
                    : ""}
                </p>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState
            title="Групп пока нет"
            description="Выберите вопросы и подтвердите состав."
          />
        )}
      </section>
    </>
  );
}

export function QuestionGroupPage({ direction }: { direction: Direction }) {
  const { id = "" } = useParams();
  const query = useMentorQuery(groupKeys.detail(direction, id), (signal) =>
    groupApi.detail(direction, id, signal),
  );
  const workspace = useMentorQuery(groupKeys.workspace(direction), (signal) =>
    groupApi.workspace(direction, signal),
  );
  return (
    <section className={styles.page}>
      <Link to={`/${direction}/question-groups`}>← Все группы</Link>
      <QueryFeedback query={query} />
      {!query.data && !query.isPending && !query.isError && (
        <InlineError
          message="Группа недоступна или доступ изменился"
          onRetry={() => void query.refetch()}
        />
      )}{" "}
      {query.data && (
        <GroupEditor
          key={`${direction}:${id}`}
          direction={direction}
          group={query.data}
          workspace={workspace.data}
        />
      )}
    </section>
  );
}
const memberLabels = {
  pending: "Ожидает доставки",
  delivered: "Ссылка доставлена",
  closed: "Вопрос закрыт",
  failed: "Нужно повторить",
};
const editLabels = {
  title: "Название темы",
  resolution: "Общее решение",
  questionIds: "Состав участников",
};
function GroupEditor({
  direction,
  group,
  workspace,
}: {
  direction: Direction;
  group: Group;
  workspace: GroupWorkspace | undefined;
}) {
  const client = useQueryClient(),
    op = useGroupAction(groupKeys.detail(direction, group.id));
  const me = useMe(),
    scope = useAccessScope(me.data?.id);
  const team =
    scope.data?.teams.find((t) => t.id === group.team_id)?.name ??
    group.team_id;
  const [edit, setEdit] = useState(() => groupEdit(group));
  const { title, resolution, questionIds: selected } = edit.values;
  const { revision } = edit;
  const hasConflicts = editFields.some((field) => edit.conflicts[field]);
  function change<K extends EditField>(
    field: K,
    value: (typeof edit.values)[K],
  ) {
    setEdit((current) => ({
      ...current,
      values: { ...current.values, [field]: value },
    }));
  }
  const published =
    !!group.draft?.document_id ||
    !!group.published_at ||
    !!group.publication_error;
  const candidates = [
    ...group.members.map((m) => ({
      id: m.id,
      question: m.question?.question ?? "Вопрос недоступен",
      student_name: m.question?.student_name ?? "",
    })),
    ...(workspace?.candidates ?? []).filter(
      (q) =>
        q.team_id === group.team_id &&
        !group.members.some((m) => m.id === q.id),
    ),
  ];
  function variant(field: EditField, server: boolean) {
    const values = server ? edit.base : edit.values;
    return field === "questionIds"
      ? values.questionIds
          .map(
            (id) =>
              candidates.find((q) => q.id === id)?.question ??
              "Вопрос недоступен",
          )
          .join("; ") || "Нет участников"
      : values[field];
  }
  function accept(g: Group, saved = false, archive = false) {
    client.setQueryData(groupKeys.detail(direction, g.id), g);
    setEdit((current) =>
      saved ? groupEdit(g) : refreshGroupEdit(current, g, archive),
    );
    void client.invalidateQueries({ queryKey: groupKeys.workspace(direction) });
  }
  function save() {
    if (hasConflicts) return;
    const body: GroupInput = {
      operation_id: group.id,
      revision,
      title,
      resolution,
      question_ids: selected,
    };
    op.run(
      (signal) => groupApi.save(direction, group.id, body, signal),
      async (signal) => {
        const g = await groupApi.detail(direction, group.id, signal);
        return g.revision > revision &&
          g.title === title.trim() &&
          g.resolution === resolution.trim() &&
          g.members
            .map((m) => m.id)
            .sort()
            .join() === selected.toSorted().join()
          ? g
          : undefined;
      },
      (g) => accept(g, true),
    );
  }
  function deliver(close: boolean) {
    op.run(
      (signal) =>
        groupApi.deliver(direction, group.id, group.revision, close, signal),
      (signal) => groupApi.detail(direction, group.id, signal),
      (g) => accept(g),
    );
  }
  function archive() {
    const body: GroupInput = {
      operation_id: group.id,
      revision: group.revision,
      title: group.title,
      resolution: group.resolution,
      question_ids: [],
    };
    op.run(
      (signal) => groupApi.save(direction, group.id, body, signal),
      async (signal) => {
        const g = await groupApi.detail(direction, group.id, signal);
        return g.members.length === 0 ? g : undefined;
      },
      (g) => {
        accept(g, false, true);
      },
    );
  }
  return (
    <>
      <header>
        <h1>{group.title}</h1>
        <p>
          {team} · {group.direction === "hr" ? "HR" : "Наставник"} · версия
          состава {group.revision}
        </p>
      </header>
      <form
        className={styles.card}
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <h2>Состав и общее решение</h2>
        {hasConflicts && (
          <section aria-label="Конфликт изменений" className={styles.card}>
            <h3>Изменения в другой вкладке</h3>
            <p role="status">
              Одни и те же поля изменены у вас и на сервере. Сравните варианты и
              выберите каждый перед сохранением. Остальные поля обновлены без
              потери ваших правок.
            </p>
            {editFields
              .filter((field) => edit.conflicts[field])
              .map((field) => (
                <div role="group" aria-label={editLabels[field]} key={field}>
                  <h4>{editLabels[field]}</h4>
                  <p>Ваш вариант: {variant(field, false)}</p>
                  <p>На сервере: {variant(field, true)}</p>
                  {published && field !== "questionIds" && (
                    <p>
                      Публикация уже началась: можно принять только серверный
                      текст. Ваш вариант остаётся здесь для копирования.
                    </p>
                  )}
                  <div className={styles.row}>
                    <Button
                      variant="secondary"
                      disabled={op.busy}
                      aria-label={`${editLabels[field]}: использовать серверный вариант`}
                      onClick={() =>
                        setEdit((current) =>
                          chooseGroupEdit(current, field, true),
                        )
                      }
                    >
                      Использовать серверный вариант
                    </Button>
                    <Button
                      variant="secondary"
                      disabled={
                        op.busy || (published && field !== "questionIds")
                      }
                      aria-label={`${editLabels[field]}: оставить мои правки`}
                      onClick={() =>
                        setEdit((current) =>
                          chooseGroupEdit(current, field, false),
                        )
                      }
                    >
                      Оставить мои правки
                    </Button>
                  </div>
                </div>
              ))}
          </section>
        )}
        <TextField
          label="Название темы"
          value={title}
          required
          maxLength={200}
          disabled={published || op.busy || edit.conflicts.title}
          onChange={(e) => change("title", e.target.value)}
        />
        <label className={styles.field}>
          Общее решение без личных подробностей
          <textarea
            value={resolution}
            maxLength={8000}
            disabled={published || op.busy || edit.conflicts.resolution}
            onChange={(e) => change("resolution", e.target.value)}
          />
        </label>
        <fieldset>
          <legend>Подтверждённые участники</legend>
          {candidates.map((q) => (
            <label className={styles.check} key={q.id}>
              <input
                type="checkbox"
                checked={selected.includes(q.id)}
                disabled={
                  op.busy ||
                  edit.conflicts.questionIds ||
                  (published && !group.members.some((m) => m.id === q.id))
                }
                onChange={(e) =>
                  change(
                    "questionIds",
                    e.target.checked
                      ? [...selected, q.id]
                      : selected.filter((id) => id !== q.id),
                  )
                }
              />
              <span>
                {q.question}
                <small>{q.student_name}</small>
              </span>
            </label>
          ))}
        </fieldset>
        <p>
          {published
            ? "После начала публикации текст фиксируется. Можно убрать участника или обновить проверку версий оставшихся вопросов."
            : "Отредактируйте общий текст: он не должен раскрывать личные обстоятельства стажёров."}
        </p>
        <div className={styles.row}>
          <Button
            type="submit"
            disabled={hasConflicts || !selected.length || selected.length > 30}
            isLoading={op.busy}
          >
            Сохранить состав
          </Button>
          <Button
            variant="secondary"
            disabled={op.busy}
            onClick={() =>
              op.run(
                (signal) => groupApi.detail(direction, group.id, signal),
                async () => undefined,
                (g) => accept(g),
              )
            }
          >
            Перечитать версию
          </Button>
        </div>
      </form>
      <section className={styles.card}>
        <h2>Инструкция и доставка</h2>
        {group.publication_error && (
          <InlineError message={group.publication_error} />
        )}{" "}
        {group.resolution ? (
          <MarkdownContent content={group.resolution} />
        ) : (
          <p>Сначала сохраните общее решение.</p>
        )}
        {group.members.length > 0 &&
          group.resolution &&
          !group.publication_error && (
            <Link to={`/${direction}/question-groups/${group.id}/instruction`}>
              Подготовить общую инструкцию
            </Link>
          )}
        <p>
          Публикация не закрывает вопросы. Сначала подтвердите доставку
          доступного материала, затем при необходимости явно закройте обращения.
        </p>
        <div className={styles.row}>
          <Button
            disabled={
              op.busy ||
              hasConflicts ||
              !group.members.length ||
              group.draft?.status !== "ready"
            }
            onClick={() => deliver(false)}
          >
            Доставить или повторить ссылки
          </Button>
          <Button
            variant="secondary"
            disabled={op.busy || hasConflicts || !group.delivered_count}
            onClick={() => deliver(true)}
          >
            Закрыть доставленные вопросы
          </Button>
        </div>
        <ul>
          {group.members.map((m) => (
            <li key={m.id}>
              <strong>
                {m.question?.student_name ?? "Участник недоступен"}
              </strong>
              <p>
                {m.question?.question ??
                  "Содержимое скрыто после изменения доступа"}
              </p>
              <p>{memberLabels[m.status]}</p>
              {m.error && <p role="alert">{m.error}</p>}
            </li>
          ))}
        </ul>
        <p>
          Чтобы объединить уже связанные группы или подготовить новое решение,
          сначала архивируйте их состав, затем создайте новую группу и проверьте
          текст заново. История и доставленные ссылки сохранятся.
        </p>
        {group.members.length > 0 ? (
          <Button
            variant="secondary"
            disabled={op.busy || hasConflicts}
            onClick={archive}
          >
            Архивировать состав
          </Button>
        ) : (
          <p role="status">
            Состав архивирован. Открытые вопросы доступны для новой группы.
          </p>
        )}
      </section>
      <section className={styles.card}>
        <h2>Польза и история</h2>
        <p>
          В группе: {group.members.length}. Доставлено: {group.delivered_count}.
          Повторных вопросов после публикации:{" "}
          {group.repeat_count_limited ? "не менее " : ""}
          {group.repeat_count}.
        </p>
        <p>
          Повторы считаются по названию темы в текущей доступной области
          специалиста; одинаковый вопрос учитывается один раз.
        </p>
        <details>
          <summary>История изменений</summary>
          <ol>
            {group.history.map((item, i) => (
              <li key={i}>
                {item.event === "composition_confirmed"
                  ? "Подтверждён состав"
                  : item.event === "group_instruction_delivered"
                    ? "Доставлена инструкция"
                    : item.event === "owner_changed"
                      ? "Изменился ответственный"
                      : "Закрыт вопрос"}{" "}
                · {new Date(item.created_at).toLocaleString("ru-RU")} ·{" "}
                {item.question_ids.length} вопросов
              </li>
            ))}
          </ol>
        </details>
      </section>
      {op.error && <InlineError message={op.error} />}
    </>
  );
}
