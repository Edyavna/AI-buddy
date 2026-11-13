import { expect, it, vi, afterEach } from "vitest";
import { screen, fireEvent, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithQuery } from "@/test/render";
import { abortSessionRequests } from "@/shared/api/session-signal";
import { ResolutionDraftPage } from "@/features/resolution-drafts/ResolutionDraftPage";
import { draftKey, type Draft } from "@/features/resolution-drafts/api";
import { QuestionGroupsPage, QuestionGroupPage } from "./QuestionGroupsPage";
import {
  groupApi,
  groupKeys,
  type Group,
  type GroupInput,
  type GroupWorkspace,
} from "./api";

const questions = [
  {
    id: "q1",
    team_id: "team",
    question: "Как оформить доступ к сервису?",
    student_id: "intern1",
    student_name: "Анна",
    step_id: null,
    revision: 1,
    status: "in_progress" as const,
    created_at: "2026-11-13T12:00:00Z",
  },
  {
    id: "q2",
    team_id: "team",
    question: "Нужен доступ к сервису",
    student_id: "intern2",
    student_name: "Борис",
    step_id: "step",
    revision: 1,
    status: "new" as const,
    created_at: "2026-11-13T12:00:00Z",
  },
];
const makeGroup = (changes: Partial<Group> = {}): Group => ({
  id: "g",
  author_id: "mentor",
  direction: "mentor",
  team_id: "team",
  title: "Доступ к сервису",
  resolution: "Общий порядок без личных сведений",
  revision: 1,
  published_at: null,
  members: questions.map((q) => ({
    id: q.id,
    question: q,
    status: "pending",
    error: null,
  })),
  history: [
    {
      event: "composition_confirmed",
      question_ids: ["q1", "q2"],
      created_at: "2026-11-13T12:00:00Z",
    },
  ],
  draft: null,
  delivered_count: 0,
  repeat_count: 0,
  repeat_count_limited: false,
  ...changes,
});
const error = (status: number) =>
  HttpResponse.json(
    {
      error: {
        code: "FAIL",
        message: status === 409 ? "Состав изменился" : "Сервис недоступен",
        trace_id: "test",
      },
    },
    { status },
  );
afterEach(() => vi.restoreAllMocks());
function start(
  detail = false,
  direction: "mentor" | "hr" = "mentor",
  initial: Group | null = detail ? makeGroup({ direction }) : null,
) {
  const state: {
    group: Group | null;
    limited: boolean;
    suggestions: GroupWorkspace["suggestions"];
  } = {
    group: initial,
    limited: false,
    suggestions: [
      {
        question_ids: ["q1", "q2"],
        reason: "Общие слова темы: доступ, сервис.",
        score: 0.5,
      },
    ],
  };
  let creates = 0,
    deliveries = 0;
  const root = `*/api/v1/${direction}/question-groups`;
  server.use(
    http.get("*/api/v1/me/access-scope", () =>
      HttpResponse.json({
        version: "1",
        teams: [{ id: "team", name: "Команда разработки" }],
      }),
    ),
    http.get(root, () =>
      HttpResponse.json({
        groups: state.group ? [state.group] : [],
        candidates: state.group ? [] : questions,
        suggestions: state.suggestions,
        limited: state.limited,
      }),
    ),
    http.get(`${root}/:id`, () =>
      state.group ? HttpResponse.json(state.group) : error(404),
    ),
    http.post(root, async ({ request }) => {
      creates++;
      const body = (await request.json()) as GroupInput;
      state.group = makeGroup({
        id: body.operation_id,
        title: body.title,
        resolution: body.resolution,
        direction,
        members: makeGroup().members.filter((m) =>
          body.question_ids.includes(m.id),
        ),
      });
      return HttpResponse.json(state.group);
    }),
    http.put(`${root}/:id`, async ({ request }) => {
      const body = (await request.json()) as GroupInput;
      state.group = makeGroup({
        ...state.group,
        revision: body.revision + 1,
        title: body.title,
        resolution: body.resolution,
        members: state.group!.members.filter((m) =>
          body.question_ids.includes(m.id),
        ),
      });
      return HttpResponse.json(state.group);
    }),
    http.post(`${root}/:id/deliver`, () => {
      deliveries++;
      state.group = makeGroup({
        ...state.group,
        delivered_count: 1,
        members: state.group!.members.map((m, i) => ({
          ...m,
          status: i ? "failed" : "delivered",
          error: i ? "Проверьте доступ и повторите" : null,
          question: i ? null : m.question,
        })),
      });
      return HttpResponse.json(state.group);
    }),
    http.post(`${root}/:id/close`, () => {
      state.group = makeGroup({
        ...state.group,
        members: state.group!.members.map((m) => ({
          ...m,
          status: m.status === "delivered" ? "closed" : m.status,
        })),
        history: [
          {
            event: "group_question_closed",
            question_ids: ["q1"],
            created_at: "2026-11-13T12:00:00Z",
          },
        ],
      });
      return HttpResponse.json(state.group);
    }),
  );
  const router = createMemoryRouter(
    [
      {
        path: "/:direction/question-groups",
        element: <QuestionGroupsPage direction={direction} />,
      },
      {
        path: "/:direction/question-groups/:id",
        element: <QuestionGroupPage direction={direction} />,
      },
      {
        path: "/:direction/question-groups/:id/instruction",
        element: <ResolutionDraftPage direction={direction} group />,
      },
    ],
    { initialEntries: [`/${direction}/question-groups${detail ? "/g" : ""}`] },
  );
  return {
    ...renderWithQuery(<RouterProvider router={router} />),
    state,
    root,
    router,
    counts: () => ({ creates, deliveries }),
  };
}
async function choose() {
  await screen.findByRole("heading", { name: "Подтвердить состав группы" });
  fireEvent.click(
    screen.getByRole("button", { name: "Выбрать предложение 1" }),
  );
  fireEvent.change(screen.getByLabelText("Название темы"), {
    target: { value: "Доступ к сервису" },
  });
  fireEvent.change(
    screen.getByLabelText("Общее решение без личных подробностей"),
    { target: { value: "Общий порядок без личных сведений" } },
  );
}

it.each(["mentor", "hr"] as const)(
  "creates a reviewed group and persists its composition for %s",
  async (direction) => {
    const { router, counts } = start(false, direction);
    await choose();
    fireEvent.click(screen.getByRole("button", { name: "Подтвердить группу" }));
    await screen.findByRole("heading", { name: "Состав и общее решение" });
    expect(router.state.location.pathname).toMatch(/question-groups\//);
    expect(counts().creates).toBe(1);
    expect(
      await screen.findByText(
        "Команда разработки · " +
          (direction === "hr" ? "HR" : "Наставник") +
          " · версия состава 1",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("checkbox", { name: /Нужен доступ/ }));
    fireEvent.click(screen.getByRole("button", { name: "Сохранить состав" }));
    await screen.findByText(/В группе: 1\./);
    expect(
      screen.getByRole("link", { name: "Подготовить общую инструкцию" }),
    ).toBeInTheDocument();
  },
);
it("permits manual singleton selection, labels empty suggestions and the bounded window", async () => {
  const { state, queryClient } = start();
  state.suggestions = [];
  state.limited = true;
  await queryClient.invalidateQueries({
    queryKey: groupKeys.workspace("mentor"),
  });
  await screen.findByText(/Убедительных совпадений нет/);
  expect(
    screen.getByText("Показаны последние 100 доступных вопросов."),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("checkbox", { name: /Как оформить/ }));
  fireEvent.click(screen.getByRole("checkbox", { name: /Как оформить/ }));
  expect(
    screen.getByRole("button", { name: "Подтвердить группу" }),
  ).toBeDisabled();
});
it("shows empty candidates and existing groups without inventing suggestions", async () => {
  const { root, queryClient } = start();
  await screen.findByRole("heading", { name: "Предложения для проверки" });
  server.use(
    http.get(root, () =>
      HttpResponse.json({
        groups: [],
        candidates: [],
        suggestions: [],
        limited: false,
      }),
    ),
  );
  await queryClient.invalidateQueries({
    queryKey: groupKeys.workspace("mentor"),
  });
  await screen.findByText("Подходящих открытых вопросов пока нет.");
  expect(screen.getByText("Групп пока нет")).toBeInTheDocument();
});
it("reconciles a lost creation response with the same UUID", async () => {
  const { root, state, router } = start();
  server.use(
    http.post(root, async ({ request }) => {
      const body = (await request.json()) as GroupInput;
      state.group = makeGroup({ id: body.operation_id });
      return HttpResponse.error();
    }),
  );
  await choose();
  fireEvent.click(screen.getByRole("button", { name: "Подтвердить группу" }));
  await screen.findByRole("heading", { name: "Состав и общее решение" });
  expect(router.state.location.pathname).toContain(state.group!.id);
});
it("keeps an unknown creation immutable and offers explicit composition changes", async () => {
  const { root } = start();
  server.use(
    http.post(root, () => HttpResponse.error()),
    http.get(`${root}/:id`, () => error(404)),
  );
  await choose();
  fireEvent.click(screen.getByRole("button", { name: "Подтвердить группу" }));
  await screen.findByRole("alert");
  expect(screen.getByLabelText("Название темы")).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Изменить состав" }));
  expect(screen.getByLabelText("Название темы")).toBeEnabled();
});
it("delivers partially without closing automatically and lets the specialist close delivered questions explicitly", async () => {
  const initial = makeGroup({
    draft: {
      revision: 1,
      title: "Инструкция",
      content: "Текст",
      source: {
        id: "g",
        direction: "mentor",
        team_id: "team",
        question: "Тема",
        resolution: "Ответ",
        step_id: null,
        revision: 1,
      },
      team_ids: ["team"],
      target_document_id: null,
      base_version: null,
      base_content: "",
      document_id: "doc",
      document_version: 1,
      status: "ready",
      error: null,
    },
    published_at: "2026-11-13T12:01:00Z",
  });
  const { counts } = start(true, "mentor", initial);
  await screen.findByRole("heading", { name: "Состав и общее решение" });
  expect(screen.getByLabelText("Название темы")).toBeDisabled();
  fireEvent.click(
    screen.getByRole("button", { name: "Доставить или повторить ссылки" }),
  );
  await screen.findByText("Нужно повторить");
  expect(screen.getByText("Ссылка доставлена")).toBeInTheDocument();
  expect(screen.queryByText("Вопрос закрыт")).not.toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: "Закрыть доставленные вопросы" }),
  );
  await screen.findByText("Вопрос закрыт");
  expect(counts().deliveries).toBe(1);
  fireEvent.click(screen.getByText("История изменений"));
  expect(screen.getByText(/Закрыт вопрос ·/)).toBeInTheDocument();
});
it("preserves edits on conflict and requires explicit refresh of the group version", async () => {
  const { root, state } = start(true);
  await screen.findByRole("heading", { name: "Состав и общее решение" });
  server.use(http.put(`${root}/:id`, () => error(409)));
  fireEvent.change(screen.getByLabelText("Название темы"), {
    target: { value: "Мой текст" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить состав" }));
  await screen.findByRole("alert");
  state.group = makeGroup({ revision: 2 });
  fireEvent.click(screen.getByRole("button", { name: "Перечитать версию" }));
  await screen.findByText(/версия состава 2/);
  expect(screen.getByLabelText("Название темы")).toHaveValue("Мой текст");
});
it.each([401, 403, 404])(
  "hides private group details after %i, including a subsequent 503",
  async (status) => {
    const { root, queryClient } = start(true);
    await screen.findByRole("heading", { name: "Состав и общее решение" });
    server.use(http.get(`${root}/:id`, () => error(status)));
    await queryClient.invalidateQueries({
      queryKey: groupKeys.detail("mentor", "g"),
    });
    await waitFor(() =>
      expect(
        screen.queryByRole("heading", { name: "Состав и общее решение" }),
      ).not.toBeInTheDocument(),
    );
    server.use(http.get(`${root}/:id`, () => error(503)));
    await queryClient.invalidateQueries({
      queryKey: groupKeys.detail("mentor", "g"),
    });
    expect(
      screen.queryByText("Общий порядок без личных сведений"),
    ).not.toBeInTheDocument();
  },
);
it("does not accept a late create result after logout", async () => {
  const { queryClient, router } = start();
  let resolve!: (g: Group) => void;
  vi.spyOn(groupApi, "save").mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  await choose();
  fireEvent.click(screen.getByRole("button", { name: "Подтвердить группу" }));
  await waitFor(() => expect(resolve).toBeDefined());
  abortSessionRequests(queryClient);
  resolve(makeGroup());
  await new Promise((r) => setTimeout(r, 20));
  expect(router.state.location.pathname).toBe("/mentor/question-groups");
});
it("reconciles an uncertain update and uses current per-recipient statuses for delivery", async () => {
  const { root, state } = start(true);
  await screen.findByRole("heading", { name: "Состав и общее решение" });
  server.use(
    http.put(`${root}/:id`, async ({ request }) => {
      const body = (await request.json()) as GroupInput;
      state.group = makeGroup({
        title: body.title,
        resolution: body.resolution,
        revision: 2,
      });
      return error(503);
    }),
  );
  fireEvent.change(screen.getByLabelText("Название темы"), {
    target: { value: "Обновлённая тема" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить состав" }));
  await screen.findByRole("heading", { name: "Обновлённая тема" });
});
it("revokes group data when a mutation is denied", async () => {
  const { root } = start(true);
  await screen.findByRole("heading", { name: "Состав и общее решение" });
  server.use(http.put(`${root}/:id`, () => error(403)));
  fireEvent.click(screen.getByRole("button", { name: "Сохранить состав" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("heading", { name: "Состав и общее решение" }),
    ).not.toBeInTheDocument(),
  );
});
it("keeps a late save hidden after access revocation and a subsequent 503", async () => {
  const { root, queryClient } = start(true);
  await screen.findByRole("heading", { name: "Состав и общее решение" });
  let resolve!: (g: Group) => void;
  vi.spyOn(groupApi, "save").mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Сохранить состав" }));
  await waitFor(() => expect(resolve).toBeDefined());
  server.use(http.get(`${root}/:id`, () => error(403)));
  await queryClient.invalidateQueries({
    queryKey: groupKeys.detail("mentor", "g"),
  });
  resolve(makeGroup({ title: "Поздний закрытый текст" }));
  server.use(http.get(`${root}/:id`, () => error(503)));
  await queryClient.invalidateQueries({
    queryKey: groupKeys.detail("mentor", "g"),
  });
  expect(queryClient.getQueryData(groupKeys.detail("mentor", "g"))).toBeNull();
  expect(screen.queryByText("Поздний закрытый текст")).not.toBeInTheDocument();
});
it("keeps group history when its publication becomes unavailable and explicitly archives its composition", async () => {
  const { state, queryClient } = start(true);
  await screen.findByRole("heading", { name: "Состав и общее решение" });
  state.group = makeGroup({
    published_at: "2026-11-13T12:00:00Z",
    publication_error: "Опубликованная версия недоступна",
    draft: null,
    history: [
      {
        event: "owner_changed",
        question_ids: ["q1"],
        created_at: "2026-11-13T12:00:00Z",
      },
    ],
  });
  await queryClient.invalidateQueries({
    queryKey: groupKeys.detail("mentor", "g"),
  });
  await screen.findByText("Опубликованная версия недоступна");
  expect(screen.getByLabelText("Название темы")).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "Доставить или повторить ссылки" }),
  ).toBeDisabled();
  expect(
    screen.queryByRole("link", { name: "Подготовить общую инструкцию" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByText("История изменений"));
  expect(screen.getByText(/Изменился ответственный/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Архивировать состав" }));
  await screen.findByText(
    "Состав архивирован. Открытые вопросы доступны для новой группы.",
  );
  expect(state.group?.members).toHaveLength(0);
});
it.each(["mentor", "hr"] as const)(
  "uses the shared instruction editor with isolated group storage for %s",
  async (direction) => {
    const { root, state, queryClient } = start(true, direction);
    await screen.findByRole("heading", { name: "Состав и общее решение" });
    const source = {
      id: "g",
      direction,
      team_id: "team",
      question: "Общая тема",
      resolution: "Общее решение",
      step_id: null,
      revision: 1,
    };
    let draft: Draft | null = null;
    server.use(
      http.get(`${root}/g/instruction-draft`, () =>
        HttpResponse.json({ source, draft, targets: [] }),
      ),
      http.post(`${root}/g/instruction-draft/generate`, () =>
        HttpResponse.json({
          title: "Общая инструкция",
          content: "Общий порядок без личных подробностей",
          mode: "model",
        }),
      ),
      http.put(`${root}/g/instruction-draft`, async ({ request }) => {
        const body = (await request.json()) as Pick<
          Draft,
          | "title"
          | "content"
          | "revision"
          | "target_document_id"
          | "base_version"
        >;
        draft = {
          ...body,
          revision: 1,
          source,
          team_ids: ["team"],
          base_content: "",
          document_id: null,
          document_version: null,
          status: "draft",
          error: null,
        };
        return HttpResponse.json(draft);
      }),
      http.post(`${root}/g/instruction-draft/publish`, () => {
        draft = {
          ...draft!,
          document_id: "doc",
          document_version: 1,
          status: "ready",
        };
        state.group = { ...state.group!, draft };
        return HttpResponse.json(draft);
      }),
    );
    fireEvent.click(
      screen.getByRole("link", { name: "Подготовить общую инструкцию" }),
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Предложить текст" }),
    );
    await screen.findByText(
      "Предложение помощника. Проверьте и отредактируйте текст.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Сохранить черновик" }));
    await screen.findByText("Все изменения сохранены");
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(
      screen.getByRole("button", { name: "Опубликовать инструкцию" }),
    );
    await screen.findByText(
      "Публикация принята. Ссылка появится в вопросе после индексации.",
    );
    await queryClient.invalidateQueries({
      queryKey: draftKey(direction, "g", true),
    });
    fireEvent.click(screen.getByRole("link", { name: "← К группе вопросов" }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Доставить или повторить ссылки" }),
      ).toBeEnabled(),
    );
  },
);
