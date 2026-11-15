import { afterEach, expect, it, vi } from "vitest";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithQuery } from "@/test/render";
import { abortSessionRequests } from "@/shared/api/session-signal";
import { QuestionComposer } from "@/features/hr/QuestionComposer";
import { DocumentDrawer } from "@/features/documents";
import { queryKeys } from "@/shared/api";
import { queryAccessSignal } from "@/shared/api/query-access";
import { hrApi, type Draft, type RoutedQuestion } from "@/features/hr/api";
import { ContextSnapshot as SnapshotView } from "./ContextSnapshot";
import {
  contextKey,
  emptyContext,
  type ContextInput,
  type ContextSnapshot,
} from "./api";

it("acknowledges an opening only after the authorized body is rendered", async () => {
  let reads = 0,
    acks = 0;
  server.use(
    http.get("*/api/v1/messages/message/context-documents/doc", () => {
      reads++;
      return HttpResponse.json({
        id: "doc",
        title: "Реальный источник",
        content: "Отображённый текст",
        version: 1,
        updated_at: "2026-11-14T10:00:00Z",
      });
    }),
    http.post(
      "*/api/v1/messages/message/context-documents/doc/opened",
      async ({ request }) => {
        expect(screen.getByText("Отображённый текст")).toBeVisible();
        expect(await request.json()).toEqual({ version: 1 });
        acks++;
        return HttpResponse.json({ opened: true });
      },
    ),
  );
  const { rerender, queryClient } = renderWithQuery(
    <DocumentDrawer
      documentId={null}
      sourceMessageId="message"
      onClose={() => {}}
    />,
  );
  expect(reads).toBe(0);
  expect(acks).toBe(0);
  rerender(
    <QueryClientProvider client={queryClient}>
      <DocumentDrawer
        documentId="doc"
        sourceMessageId="message"
        onClose={() => {}}
      />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(acks).toBe(1));
});

it("does not count a canceled read that was never shown", async () => {
  let release!: (response: Response) => void,
    acks = 0;
  server.use(
    http.get(
      "*/api/v1/messages/message/context-documents/doc",
      () =>
        new Promise<Response>((r) => {
          release = r;
        }),
    ),
    http.post("*/api/v1/messages/message/context-documents/doc/opened", () => {
      acks++;
      return HttpResponse.json({ opened: true });
    }),
  );
  const { rerender, queryClient } = renderWithQuery(
    <DocumentDrawer
      documentId="doc"
      sourceMessageId="message"
      onClose={() => {}}
    />,
  );
  await waitFor(() => expect(release).toBeDefined());
  rerender(
    <QueryClientProvider client={queryClient}>
      <DocumentDrawer
        documentId={null}
        sourceMessageId="message"
        onClose={() => {}}
      />
    </QueryClientProvider>,
  );
  release(
    HttpResponse.json({
      id: "doc",
      title: "Не показан",
      content: "Не показанный текст",
      version: 1,
      updated_at: "2026-11-14T10:00:00Z",
    }),
  );
  await new Promise((r) => setTimeout(r, 30));
  expect(acks).toBe(0);
});

it("hides a rendered document when opening acknowledgement discovers revoked access, including a following 503", async () => {
  server.use(
    http.get("*/api/v1/messages/message/context-documents/doc", () =>
      HttpResponse.json({
        id: "doc",
        title: "Закрываемый источник",
        content: "Закрываемый текст",
        version: 1,
        updated_at: "2026-11-14T10:00:00Z",
      }),
    ),
    http.post("*/api/v1/messages/message/context-documents/doc/opened", () =>
      error(403),
    ),
  );
  const { queryClient } = renderWithQuery(
    <DocumentDrawer
      documentId="doc"
      sourceMessageId="message"
      onClose={() => {}}
    />,
    (client) => {
      client.setQueryData(contextKey("message", "mentor"), preview());
      client.setQueryData(["hr", "message", "message"], {
        items: [],
        drafts: [],
      });
    },
  );
  await screen.findByText("Документ недоступен или доступ изменился");
  expect(queryClient.getQueryData(contextKey("message", "mentor"))).toBeNull();
  expect(queryClient.getQueryData(["hr", "message", "message"])).toBeNull();
  expect(screen.queryByText("Закрываемый текст")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("dialog", { name: "Закрываемый источник" }),
  ).not.toBeInTheDocument();
  server.use(
    http.get("*/api/v1/messages/message/context-documents/doc", () =>
      error(503),
    ),
  );
  await queryClient.invalidateQueries({ queryKey: queryKeys.document("doc") });
  await waitFor(() =>
    expect(screen.queryByText("Закрываемый текст")).not.toBeInTheDocument(),
  );
});

const approval = "Я проверил вопрос, адресата и весь отправляемый контекст";
const preview = (direction = "mentor"): ContextSnapshot => ({
  conversation_id: "conversation",
  attempts: "",
  step: {
    id: "step",
    title: "Настроить сервис",
    revision: 2,
    status: "available",
  },
  fragments: [
    {
      message_id: "user-message",
      role: "user",
      text:
        direction === "hr"
          ? "Вопрос о кадровой форме"
          : "Я проверил конфигурацию",
      edited: false,
      status: "available",
    },
    {
      message_id: "assistant-message",
      role: "assistant",
      text: "Личный фрагмент для исключения",
      edited: false,
      status: "available",
    },
  ],
  documents: [
    {
      document_id: "doc",
      version: 1,
      title: "Инструкция сервиса",
      opened: false,
      status: "available",
    },
  ],
  limited: false,
});
const error = (status: number) =>
  HttpResponse.json(
    {
      error: {
        code: "TEST",
        message: "Ошибка проверки",
        trace_id: "context-test",
      },
    },
    { status },
  );
const snapshot = (
  input: ContextInput,
  direction = "mentor",
): ContextSnapshot => ({
  ...preview(direction),
  step: input.include_step ? preview().step : null,
  attempts: input.attempts,
  fragments: input.fragments.map((f) => ({
    ...preview(direction).fragments.find((p) => p.message_id === f.message_id)!,
    ...f,
    edited: true,
  })),
  documents: input.documents.map((d) => ({ ...preview().documents[0]!, ...d })),
});
const question = (draft: Draft): RoutedQuestion => ({
  id: "q",
  message_id: "message",
  team_id: "team",
  direction: draft.direction,
  original_direction: draft.direction,
  student: { id: "intern", name: "Стажёр" },
  specialist_id: draft.direction === "mentor" ? "mentor" : null,
  step_id: draft.context_snapshot?.include_step ? "step" : null,
  question: draft.question,
  context_excerpt: draft.context_excerpt,
  status: "new",
  revision: 1,
  resolution: null,
  document_ids: [],
  routing: null,
  applied_revision: 1,
  created_at: "2026-11-14T10:00:00Z",
  updated_at: "2026-11-14T10:00:00Z",
  ...(draft.context_snapshot
    ? { context_snapshot: snapshot(draft.context_snapshot, draft.direction) }
    : {}),
});
afterEach(() => vi.restoreAllMocks());
function start(direction: "mentor" | "hr" = "mentor") {
  const state: {
    items: RoutedQuestion[];
    drafts: Draft[];
    posted: (Draft & { operation_id: string })[];
    available: string[];
    preview: ContextSnapshot;
    routing: NonNullable<RoutedQuestion["routing"]>;
  } = {
    items: [],
    drafts: [],
    posted: [],
    available: ["mentor", "hr"],
    preview: preview(direction),
    routing: {
      direction,
      reason: "Предложение классификатора",
      clarification: null,
      split: false,
    },
  };
  const root = "*/api/v1/messages/message";
  server.use(
    http.get(root + "/questions", () =>
      HttpResponse.json({
        items: state.items,
        drafts: state.drafts,
        question: "Как настроить сервис?",
        available_directions: state.available,
        routing: state.routing,
      }),
    ),
    http.get(root + "/question-context", ({ request }) =>
      HttpResponse.json(
        new URL(request.url).searchParams.get("direction") === direction
          ? state.preview
          : preview("hr"),
      ),
    ),
    http.put(
      root + "/question-drafts/:direction",
      async ({ request, params }) => {
        const d = (await request.json()) as Draft;
        const value = { ...d, direction: params.direction as "mentor" | "hr" };
        state.drafts = [value];
        return HttpResponse.json(value);
      },
    ),
    http.post(root + "/questions", async ({ request }) => {
      const body = (await request.json()) as Draft & { operation_id: string };
      state.posted.push(body);
      const item = { ...question(body), id: state.items.length ? "q-hr" : "q" };
      state.items = [...state.items, item];
      state.drafts = [];
      return HttpResponse.json(item);
    }),
    http.get("*/api/v1/questions/q", () =>
      HttpResponse.json({ question: state.items[0], history: [] }),
    ),
    http.post("*/api/v1/questions/q/reroute", async ({ request }) => {
      const body = (await request.json()) as Draft & { operation_id: string };
      state.posted.push(body);
      state.items = [
        { ...question(body), original_direction: direction, revision: 2 },
      ];
      state.drafts = [];
      return HttpResponse.json(state.items[0]);
    }),
  );
  const props = {
    messageId: "message",
    onOpenDocument: vi.fn(),
    onOpenContextDocument: vi.fn(),
  };
  return {
    ...renderWithQuery(<QuestionComposer {...props} />),
    state,
    root,
    props,
  };
}
async function open() {
  fireEvent.click(
    screen.getByRole("button", { name: "Обратиться к специалисту" }),
  );
  await screen.findByRole("heading", {
    name: "Подтверждённые данные и выбранные фрагменты",
  });
  await screen.findByRole("checkbox", { name: /Включить сообщение 1/ });
}
async function confirm() {
  fireEvent.click(screen.getByRole("checkbox", { name: approval }));
  fireEvent.click(screen.getByRole("button", { name: "Подтвердить отправку" }));
}

it.each(["mentor", "hr"] as const)(
  "sends only the explicitly approved %s fragments and distinguishes attempts from source facts",
  async (direction) => {
    const { state, props } = start(direction);
    await open();
    expect(
      screen.getByRole("button", { name: "Подтвердить отправку" }),
    ).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Включить чекпоинт/ }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Включить сообщение 1/ }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Включить сообщение 2/ }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Включить сообщение 2/ }),
    );
    fireEvent.change(screen.getByLabelText("Резюме сообщения 1"), {
      target: { value: "Подтверждённая правка цитаты" },
    });
    fireEvent.change(
      screen.getByLabelText("Что уже пробовал — со слов стажёра"),
      { target: { value: "Перезапустил сервис — описал сам" } },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: "Открыть источник: Инструкция сервиса",
      }),
    );
    expect(props.onOpenContextDocument).toHaveBeenCalledWith("doc");
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /Инструкция сервиса.*Предложена помощником/,
      }),
    );
    expect(
      within(
        screen.getByRole("region", {
          name: "Предпросмотр отправляемого контекста",
        }),
      ).queryByText("Личный фрагмент для исключения"),
    ).not.toBeInTheDocument();
    await confirm();
    await screen.findByText(
      direction === "hr" ? "HR · Новое" : "Наставник · Новое",
    );
    expect(state.posted).toHaveLength(1);
    const sent = state.posted[0]!.context_snapshot!;
    expect(sent).toEqual({
      reviewed: true,
      include_step: true,
      step_revision: 2,
      attempts: "Перезапустил сервис — описал сам",
      fragments: [
        { message_id: "user-message", text: "Подтверждённая правка цитаты" },
      ],
      documents: [{ document_id: "doc", version: 1 }],
    });
    expect(JSON.stringify(sent)).not.toContain("Личный фрагмент");
    expect(
      await screen.findByRole("region", {
        name: "Отправленный снимок контекста",
      }),
    ).toHaveTextContent("со слов стажёра");
  },
);

it("preserves a durable draft through failed delivery and reload, then rechecks explicit approval", async () => {
  const { state, root, unmount } = start();
  await open();
  fireEvent.change(screen.getByLabelText("Контекст для специалиста"), {
    target: { value: "Мой сохранённый контекст" },
  });
  fireEvent.change(
    screen.getByLabelText("Что уже пробовал — со слов стажёра"),
    { target: { value: "Проверил настройки" } },
  );
  server.use(http.post(root + "/questions", () => error(503)));
  await confirm();
  await screen.findByRole("alert");
  expect(state.drafts[0]?.context_snapshot?.attempts).toBe(
    "Проверил настройки",
  );
  unmount();
  renderWithQuery(
    <QuestionComposer messageId="message" onOpenDocument={() => {}} />,
  );
  await open();
  expect(screen.getByLabelText("Контекст для специалиста")).toHaveValue(
    "Мой сохранённый контекст",
  );
  expect(
    screen.getByLabelText("Что уже пробовал — со слов стажёра"),
  ).toHaveValue("Проверил настройки");
  expect(screen.getByRole("checkbox", { name: approval })).not.toBeChecked();
});

it("reconciles a lost response against the exact approved snapshot without duplicate escalation", async () => {
  const { state, root } = start();
  await open();
  server.use(
    http.post(root + "/questions", async ({ request }) => {
      const body = (await request.json()) as Draft & { operation_id: string };
      state.posted.push(body);
      state.items = [question(body)];
      return HttpResponse.error();
    }),
  );
  fireEvent.change(
    screen.getByLabelText("Что уже пробовал — со слов стажёра"),
    { target: { value: "Мой точный снимок" } },
  );
  await confirm();
  await screen.findByText("Наставник · Новое");
  expect(state.posted).toHaveLength(1);
});

it("restores a manually chosen recipient and its saved context instead of the model suggestion", async () => {
  const { state, unmount } = start();
  await open();
  fireEvent.change(screen.getByRole("combobox", { name: "Адресат" }), {
    target: { value: "hr" },
  });
  await screen.findByText("Вопрос о кадровой форме");
  fireEvent.change(
    screen.getByLabelText("Что уже пробовал — со слов стажёра"),
    { target: { value: "Мой кадровый черновик" } },
  );
  fireEvent.click(screen.getByRole("button", { name: "Сохранить черновик" }));
  await waitFor(() => expect(state.drafts[0]?.direction).toBe("hr"));
  unmount();
  renderWithQuery(
    <QuestionComposer messageId="message" onOpenDocument={() => {}} />,
  );
  await open();
  expect(screen.getByRole("combobox", { name: "Адресат" })).toHaveValue("hr");
  expect(
    screen.getByLabelText("Что уже пробовал — со слов стажёра"),
  ).toHaveValue("Мой кадровый черновик");
  expect(screen.getByRole("checkbox", { name: approval })).not.toBeChecked();
});

it("clears the context and approval when changing direction and rerouting", async () => {
  const { state } = start();
  await open();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /Включить сообщение 1/ }),
  );
  fireEvent.change(screen.getByLabelText("Контекст для специалиста"), {
    target: { value: "Не переносить в другое направление" },
  });
  fireEvent.click(screen.getByRole("checkbox", { name: approval }));
  fireEvent.change(screen.getByRole("combobox", { name: "Адресат" }), {
    target: { value: "hr" },
  });
  expect(screen.getByLabelText("Контекст для специалиста")).toHaveValue("");
  expect(screen.getByRole("checkbox", { name: approval })).not.toBeChecked();
  await screen.findByText("Вопрос о кадровой форме");
  expect(screen.queryByLabelText("Резюме сообщения 1")).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Вопрос"), {
    target: { value: "Только кадровая часть" },
  });
  await confirm();
  await screen.findByText("HR · Новое");
  expect(state.posted[0]?.context_snapshot?.fragments).toEqual([]);
  fireEvent.click(screen.getByRole("button", { name: "Изменить адресата" }));
  expect(screen.getByRole("checkbox", { name: approval })).not.toBeChecked();
  fireEvent.click(screen.getByRole("button", { name: "Отмена" }));
  fireEvent.click(screen.getByRole("button", { name: "Изменить адресата" }));
  fireEvent.click(screen.getByRole("checkbox", { name: approval }));
  fireEvent.click(
    screen.getByRole("button", { name: "Подтвердить перенаправление" }),
  );
  await screen.findByText("Наставник · Новое");
});

it("splits a clarification into separately approved mentor and HR snapshots", async () => {
  const { state } = start();
  state.routing = {
    direction: "needs_clarification",
    reason: "Две темы",
    clarification: "Уточните рабочую и кадровую части",
    split: true,
  };
  await open();
  await screen.findByText(/Уточните рабочую и кадровую части/);
  fireEvent.change(screen.getByLabelText("Вопрос"), {
    target: { value: "Рабочая часть" },
  });
  fireEvent.change(
    screen.getByLabelText("Что уже пробовал — со слов стажёра"),
    { target: { value: "Не копировать в HR" } },
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: /Включить сообщение 1/ }),
  );
  await confirm();
  await screen.findByText("Наставник · Новое");
  await waitFor(() => expect(screen.getByLabelText("Вопрос")).toHaveValue(""));
  expect(screen.getByRole("combobox", { name: "Адресат" })).toHaveValue("hr");
  expect(
    screen.getByLabelText("Что уже пробовал — со слов стажёра"),
  ).toHaveValue("");
  expect(screen.getByRole("checkbox", { name: approval })).not.toBeChecked();
  fireEvent.change(screen.getByLabelText("Вопрос"), {
    target: { value: "Кадровая часть" },
  });
  await screen.findByText("Вопрос о кадровой форме");
  await confirm();
  await screen.findByText("HR · Новое");
  expect(state.posted).toHaveLength(2);
  expect(state.posted[1]?.context_snapshot?.fragments).toEqual([]);
  expect(state.posted[1]?.context_snapshot?.attempts).toBe("");
});

it("requires renewed approval if a remotely saved default draft changes", async () => {
  const { state, queryClient } = start();
  await open();
  fireEvent.click(screen.getByRole("checkbox", { name: approval }));
  state.drafts = [
    {
      direction: "mentor",
      question: "Свежий вопрос другой вкладки",
      context_excerpt: "Свежий текст",
      context_snapshot: emptyContext(),
    },
  ];
  await queryClient.invalidateQueries({
    queryKey: ["hr", "message", "message"],
  });
  await waitFor(() =>
    expect(screen.getByLabelText("Вопрос")).toHaveValue(
      "Свежий вопрос другой вкладки",
    ),
  );
  expect(screen.getByRole("checkbox", { name: approval })).not.toBeChecked();
  expect(
    screen.getByRole("button", { name: "Подтвердить отправку" }),
  ).toBeDisabled();
});

it("supports manual context when extraction fails and keeps the draft when no HR is assigned", async () => {
  const { state, root, queryClient } = start("hr");
  state.available = ["mentor"];
  server.use(http.get(root + "/question-context", () => error(503)));
  fireEvent.click(
    screen.getByRole("button", { name: "Обратиться к специалисту" }),
  );
  await screen.findByText(/Можно заполнить резюме и попытки вручную/);
  fireEvent.change(screen.getByLabelText("Контекст для специалиста"), {
    target: { value: "Ручное резюме" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить черновик" }));
  await waitFor(() => expect(state.drafts).toHaveLength(1));
  expect(
    screen.getByRole("button", { name: "Подтвердить отправку" }),
  ).toBeDisabled();
  state.available = ["mentor", "hr"];
  await queryClient.invalidateQueries({
    queryKey: ["hr", "message", "message"],
  });
  await confirm();
  await screen.findByText("HR · Новое");
});

it.each([401, 403, 404])(
  "does not expose cached fragments after %i followed by 503 or accept a late send",
  async (status) => {
    const { root, queryClient } = start();
    await open();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Включить сообщение 2/ }),
    );
    server.use(http.get(root + "/question-context", () => error(status)));
    await queryClient.invalidateQueries({
      queryKey: contextKey("message", "mentor"),
    });
    await waitFor(() =>
      expect(
        screen.queryByText("Личный фрагмент для исключения"),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("checkbox", { name: approval })).toBeDisabled();
    server.use(http.get(root + "/question-context", () => error(503)));
    await queryClient.invalidateQueries({
      queryKey: contextKey("message", "mentor"),
    });
    expect(
      screen.queryByText("Личный фрагмент для исключения"),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: approval })).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Удалить недоступный фрагмент" }),
    );
    expect(screen.getByRole("checkbox", { name: approval })).toBeDisabled();
  },
);

it("blocks stale checkpoint/document versions until references are removed and allows an explicit refresh", async () => {
  const { state, queryClient } = start();
  await open();
  fireEvent.click(screen.getByRole("checkbox", { name: /Включить чекпоинт/ }));
  fireEvent.click(
    screen.getByRole("checkbox", { name: /Инструкция сервиса.*Предложена/ }),
  );
  state.preview = {
    ...state.preview,
    step: { ...state.preview.step!, revision: 3 },
    documents: [{ ...state.preview.documents[0]!, version: 2, opened: true }],
    limited: true,
  };
  await queryClient.invalidateQueries({
    queryKey: contextKey("message", "mentor"),
  });
  await screen.findByText(/Показаны до 12/);
  expect(screen.getByRole("checkbox", { name: approval })).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: /Включить чекпоинт/ }));
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Удалить недоступную версию источника",
    }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Обновить доступные фрагменты" }),
  );
  await waitFor(() =>
    expect(screen.getByRole("checkbox", { name: approval })).toBeEnabled(),
  );
  expect(
    screen.getByRole("checkbox", { name: /версия 2.*Открыта вами/ }),
  ).not.toBeChecked();
});

it("never accepts an ignored late success after logout", async () => {
  const { queryClient } = start();
  await open();
  let resolve!: (q: RoutedQuestion) => void;
  vi.spyOn(hrApi, "send").mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  await confirm();
  await waitFor(() => expect(resolve).toBeDefined());
  abortSessionRequests(queryClient);
  resolve(
    question({
      direction: "mentor",
      question: "Закрытый поздний вопрос",
      context_excerpt: "Закрытый поздний текст",
      context_snapshot: emptyContext(),
    }),
  );
  await new Promise((r) => setTimeout(r, 30));
  expect(screen.queryByText("Закрытый поздний вопрос")).not.toBeInTheDocument();
});

it("hides explicitly unavailable metadata and replaces a stale source version as one choice", async () => {
  const { state, queryClient } = start();
  await open();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /Включить сообщение 2/ }),
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: /Инструкция сервиса.*Предложена/ }),
  );
  state.preview = {
    ...state.preview,
    fragments: state.preview.fragments.map((f) => ({
      ...f,
      status: "unavailable",
      text: "Не раскрывать закрытый текст",
    })),
    documents: [
      {
        ...state.preview.documents[0]!,
        status: "unavailable",
        title: "Не раскрывать закрытое название",
      },
    ],
  };
  await queryClient.invalidateQueries({
    queryKey: contextKey("message", "mentor"),
  });
  expect(
    screen.queryByText("Не раскрывать закрытый текст"),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByText(/Не раскрывать закрытое название/),
  ).not.toBeInTheDocument();
  await waitFor(() =>
    expect(
      screen.queryByLabelText("Резюме сообщения 2"),
    ).not.toBeInTheDocument(),
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Удалить недоступный фрагмент" }),
  );
  state.preview = {
    ...preview(),
    documents: [{ ...preview().documents[0]!, version: 2 }],
  };
  await queryClient.invalidateQueries({
    queryKey: contextKey("message", "mentor"),
  });
  fireEvent.click(
    await screen.findByRole("checkbox", {
      name: /Инструкция сервиса.*версия 2/,
    }),
  );
  expect(
    screen.queryByRole("checkbox", {
      name: "Удалить недоступную версию источника",
    }),
  ).not.toBeInTheDocument();
  await confirm();
  await screen.findByText("Наставник · Новое");
  expect(state.posted[0]?.context_snapshot?.documents).toEqual([
    { document_id: "doc", version: 2 },
  ]);
});

it.each(["mentor", "hr", "intern"] as const)(
  "opens only the authorized snapshot document for %s and redacts revoked references",
  async (direction) => {
    const data = preview();
    server.use(
      http.get(
        `*/api/v1/${direction === "intern" ? "" : direction + "/"}questions/q/context-documents/doc`,
        () =>
          HttpResponse.json({
            id: "doc",
            title: "Точный источник",
            content: "# Авторизованный текст",
            updated_at: "2026-11-14T10:00:00Z",
          }),
      ),
    );
    const { rerender } = renderWithQuery(
      <SnapshotView
        snapshot={data}
        direction={direction === "intern" ? "mentor" : direction}
        questionId="q"
        author={direction === "intern"}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Открыть источник контекста" }),
    );
    await screen.findByRole("dialog", { name: "Точный источник" });
    fireEvent.click(screen.getByRole("button", { name: "Закрыть панель" }));
    rerender(
      <SnapshotView
        snapshot={{
          ...data,
          step: { ...data.step!, status: "unavailable", title: null },
          fragments: data.fragments.map((f) => ({
            ...f,
            text: null,
            status: "unavailable",
          })),
          documents: [
            { ...data.documents[0]!, title: null, status: "unavailable" },
          ],
        }}
        direction={direction === "intern" ? "mentor" : direction}
        questionId="q"
        author={direction === "intern"}
      />,
    );
    expect(
      screen.queryByText("Инструкция сервиса", { exact: false }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Открыть источник контекста" }),
    ).not.toBeInTheDocument();
    expect(screen.getAllByText("Фрагмент недоступен")).toHaveLength(2);
  },
);

it.each(["mentor", "hr", "intern", "source"] as const)(
  "clears parent context copies and cancels their old generation after a document denial for %s",
  async (direction) => {
    let status = 200;
    const source = direction === "source";
    const path = source
      ? "/messages/message"
      : `/${direction === "intern" ? "" : direction + "/"}questions/q`;
    server.use(
      http.get(`*/api/v1${path}/context-documents/doc`, () =>
        status === 200
          ? HttpResponse.json({
              id: "doc",
              title: "Закрываемая метаинформация",
              content: "Старый закрываемый текст",
              updated_at: "2026-11-14T10:00:00Z",
            })
          : error(status),
      ),
    );
    const parent = source
      ? contextKey("message", "mentor")
      : [direction === "mentor" ? "mentor" : "hr", "question", "q"];
    let oldScope!: AbortSignal;
    const { queryClient } = renderWithQuery(
      <DocumentDrawer
        documentId="doc"
        onClose={() => {}}
        {...(source
          ? { sourceMessageId: "message" }
          : { contextQuestion: { id: "q", direction } })}
      />,
      (client) => {
        client.setQueryData(parent, preview());
        client.setQueryData(["hr", "message", "message"], {
          items: [],
          drafts: [],
        });
        client.setQueryData(["hr", "history", "q"], { history: [] });
        oldScope = queryAccessSignal(client, parent);
      },
    );
    await screen.findByText("Старый закрываемый текст");
    status = direction === "mentor" ? 401 : direction === "intern" ? 404 : 403;
    await queryClient.invalidateQueries({
      queryKey: queryKeys.document("doc"),
    });
    await waitFor(() => expect(queryClient.getQueryData(parent)).toBeNull());
    expect(oldScope.aborted).toBe(true);
    expect(queryClient.getQueryData(["hr", "message", "message"])).toBeNull();
    status = 503;
    await queryClient.invalidateQueries({
      queryKey: queryKeys.document("doc"),
    });
    expect(
      screen.queryByText("Старый закрываемый текст"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("dialog", { name: "Закрываемая метаинформация" }),
    ).not.toBeInTheDocument();
    expect(queryClient.getQueryData(parent)).toBeNull();
  },
);

it("labels an empty snapshot without fabricating history, sources, attempts or checkpoint", () => {
  const data = {
    ...preview(),
    step: null,
    fragments: [],
    documents: [],
    attempts: "",
  };
  const { rerender } = renderWithQuery(
    <SnapshotView snapshot={data} direction="mentor" questionId="q" />,
  );
  expect(
    screen.getByText("Фрагменты диалога не передавались."),
  ).toBeInTheDocument();
  expect(screen.getByText("Источники не передавались.")).toBeInTheDocument();
  rerender(
    <SnapshotView snapshot={undefined} direction="mentor" questionId="q" />,
  );
  expect(
    screen.queryByRole("region", { name: "Отправленный снимок контекста" }),
  ).not.toBeInTheDocument();
});
