import { expect, it } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithQuery } from "@/test/render";
import { abortSessionRequests } from "@/shared/api/session-signal";
import { queryAccessSignal } from "@/shared/api/query-access";
import { MemoryPage } from "./MemoryPage";
import { MemorySuggestions } from "./MemorySuggestions";
import { memoryKeys, type Entry, type Proposal, type Option } from "./api";

const option: Option = {
  category: "work_environment",
  key: "shell",
  value: "powershell",
  text: "Использую PowerShell",
};
const bash: Option = { ...option, value: "bash", text: "Использую Bash" };
const proposal = (id = "proposal"): Proposal => ({
  id,
  ...option,
  source_message_id: "source",
  conversation_id: "conversation",
  state: "pending",
  created_at: "2026-11-14T12:00:00Z",
  entry_id: null,
  replaces: null,
});
const entry = (value = "powershell", revision = 1): Entry => ({
  id: "entry",
  ...(value === "bash" ? bash : option),
  source_message_id: "source",
  conversation_id: "conversation",
  revision,
  edited: revision > 1,
  created_at: "2026-11-14T12:00:00Z",
  updated_at: "2026-11-14T12:00:00Z",
});
const error = (status: number) =>
  HttpResponse.json(
    {
      error: {
        code: status === 409 ? "MEMORY_CHANGED" : "TEST",
        message:
          status === 409
            ? "Память изменилась. Перечитайте записи."
            : "Не удалось выполнить запрос",
        trace_id: "memory",
      },
    },
    { status },
  );
function fixture() {
  const state: {
    entries: Entry[];
    proposals: Proposal[];
    extraction: "not_requested" | "ready" | "failed" | "skipped";
    reads: number;
    selections: number;
    writes: number;
    failRead: number;
    failSelect: boolean;
    failWrite: number;
    lose: boolean;
    deleted: boolean;
  } = {
    entries: [],
    proposals: [],
    extraction: "not_requested",
    reads: 0,
    selections: 0,
    writes: 0,
    failRead: 0,
    failSelect: false,
    failWrite: 0,
    lose: false,
    deleted: false,
  };
  const workspace = () => ({
    entries: state.entries,
    pending: state.proposals.filter((p) => p.state === "pending"),
    options: [option, bash],
    context: {
      team: { id: "team", name: "Команда backend" },
      role: "intern",
      mentor: { id: "mentor", name: "Текущий наставник" },
      progress_percent: 25,
      requests: [],
    },
    profile_markdown:
      "# Персональный профиль\n" + state.entries.map((e) => e.text).join("\n"),
  });
  server.use(
    http.get("*/api/v1/memory", () => {
      state.reads++;
      return state.failRead
        ? error(state.failRead)
        : HttpResponse.json(workspace());
    }),
    http.get("*/api/v1/messages/:id/memory-proposals", () =>
      state.failRead
        ? error(state.failRead)
        : HttpResponse.json({
            state: state.extraction,
            items: state.proposals,
          }),
    ),
    http.post("*/api/v1/messages/:id/memory-proposals", () => {
      state.selections++;
      if (state.failSelect) {
        state.extraction = "failed";
        return HttpResponse.json({ state: "failed", items: [] });
      }
      state.extraction = "ready";
      if (!state.proposals.length) state.proposals = [proposal()];
      return HttpResponse.json({
        state: state.extraction,
        items: state.proposals,
      });
    }),
    http.put("*/api/v1/memory/proposals/:id", async ({ params, request }) => {
      state.writes++;
      if (state.failWrite) return error(state.failWrite);
      const body = (await request.json()) as {
        decision: "accept" | "dismiss";
        expected_entry_id: string | null;
        expected_revision: number;
      };
      const p = state.proposals.find((p) => p.id === params.id)!;
      if (p.state === "pending") {
        expect(body.expected_entry_id).toBe(p.replaces?.id ?? null);
        expect(body.expected_revision).toBe(p.replaces?.revision ?? 0);
        p.state = body.decision === "accept" ? "accepted" : "dismissed";
        if (body.decision === "accept") {
          state.entries = [entry()];
          p.entry_id = "entry";
        }
      }
      return state.lose
        ? HttpResponse.error()
        : HttpResponse.json({
            proposal: p,
            entry: p.entry_id ? (state.entries[0] ?? null) : null,
          });
    }),
    http.get("*/api/v1/memory/entries/:id", () =>
      state.entries.length
        ? HttpResponse.json(state.entries[0])
        : state.deleted
          ? HttpResponse.json(
              {
                error: {
                  code: "MEMORY_ENTRY_DELETED",
                  message: "Эта запись удалена из памяти.",
                  trace_id: "memory",
                },
              },
              { status: 404 },
            )
          : error(404),
    ),
    http.put("*/api/v1/memory/entries/:id", async ({ request }) => {
      state.writes++;
      if (state.failWrite) return error(state.failWrite);
      const body = (await request.json()) as {
        value: string;
        revision: number;
      };
      state.entries = [entry(body.value, body.revision + 1)];
      return state.lose
        ? HttpResponse.error()
        : HttpResponse.json(state.entries[0]);
    }),
    http.delete("*/api/v1/memory/entries/:id", () => {
      state.writes++;
      if (state.failWrite) return error(state.failWrite);
      state.entries = [];
      state.deleted = true;
      state.proposals = state.proposals.map((p) => ({ ...p, entry_id: null }));
      return state.lose
        ? HttpResponse.error()
        : HttpResponse.json({ deleted: true });
    }),
  );
  return { state, workspace };
}
function mountPage() {
  return renderWithQuery(
    <MemoryRouter>
      <MemoryPage />
    </MemoryRouter>,
  );
}
function mountSuggestions() {
  return renderWithQuery(
    <MemoryRouter>
      <MemorySuggestions messageId="source" />
    </MemoryRouter>,
  );
}

it("proposes source-grounded memory without saving, confirms once and restores it after reload", async () => {
  const { state } = fixture();
  const view = mountSuggestions();
  await screen.findByRole("heading", {
    name: "Запомнить для следующих диалогов?",
  });
  expect(state.entries).toEqual([]);
  expect(state.selections).toBe(1);
  expect(
    screen.getByRole("link", { name: "Исходная реплика" }),
  ).toHaveAttribute("href", "/chat/conversation#message-source");
  const button = screen.getByRole("button", {
    name: "Запомнить: Использую PowerShell",
  });
  fireEvent.click(button);
  fireEvent.click(button);
  await screen.findByText(/Предложение подтверждено/);
  expect(state.entries).toHaveLength(1);
  expect(state.writes).toBe(1);
  view.unmount();
  mountSuggestions();
  await screen.findByText(/Предложение подтверждено/);
  expect(state.selections).toBe(1);
  expect(state.writes).toBe(1);
});
it("dismisses a proposal and keeps it out of the profile", async () => {
  const { state } = fixture();
  const view = mountSuggestions();
  fireEvent.click(await screen.findByRole("button", { name: "Не сохранять" }));
  await screen.findByText("Предложение отклонено.");
  expect(state.entries).toHaveLength(0);
  view.unmount();
  mountPage();
  await screen.findByText(
    "Вы ещё ничего не сохранили. Предложения становятся памятью только после вашего подтверждения.",
  );
  expect(
    screen.queryByRole("article", { name: "Использую PowerShell" }),
  ).not.toBeInTheDocument();
});
it("shows loading, empty preferences and a fresh application context", async () => {
  const { state } = fixture();
  mountPage();
  await screen.findByText("Наставник: Текущий наставник");
  expect(screen.getByText("Прогресс плана: 25%")).toBeVisible();
  expect(screen.getByText("Команда: Команда backend")).toBeVisible();
  expect(state.selections).toBe(0);
  expect(screen.getByRole("link", { name: "← К помощнику" })).toHaveAttribute(
    "href",
    "/chat",
  );
  fireEvent.click(screen.getByText("Что учитывает помощник"));
  expect(screen.getByText(/# Персональный профиль/)).toBeVisible();
});
it.each([false, true])(
  "edits and deletes confirmed memory, including a lost response (%s)",
  async (lose) => {
    const { state } = fixture();
    state.entries = [entry()];
    state.lose = lose;
    mountPage();
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Изменить: Использую PowerShell",
      }),
    );
    fireEvent.change(
      screen.getByRole("combobox", { name: "Новое предпочтение" }),
      { target: { value: "bash" } },
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Сохранить предпочтение" }),
    );
    await screen.findByRole("heading", { name: "Использую Bash" });
    expect(state.entries[0]?.value).toBe("bash");
    expect(screen.getByText(/Изменено вами/)).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "Удалить: Использую Bash" }),
    );
    fireEvent.click(
      within(
        screen.getByRole("group", { name: "Подтверждение удаления" }),
      ).getByRole("button", { name: "Отмена" }),
    );
    expect(
      screen.queryByRole("group", { name: "Подтверждение удаления" }),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Удалить: Использую Bash" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Удалить из памяти" }));
    await screen.findByText(
      "Вы ещё ничего не сохранили. Предложения становятся памятью только после вашего подтверждения.",
    );
    expect(state.entries).toHaveLength(0);
    expect(state.writes).toBe(2);
  },
);
it("preserves an edit on conflict and reopens the fresh value only after explicit rereading", async () => {
  const { state } = fixture();
  state.entries = [entry()];
  state.failWrite = 409;
  mountPage();
  fireEvent.click(
    await screen.findByRole("button", {
      name: "Изменить: Использую PowerShell",
    }),
  );
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "bash" } });
  fireEvent.click(
    screen.getByRole("button", { name: "Сохранить предпочтение" }),
  );
  await screen.findByText("Память изменилась. Перечитайте записи.");
  expect(screen.getByRole("combobox")).toHaveValue("bash");
  fireEvent.click(screen.getByRole("button", { name: "Перечитать память" }));
  await waitFor(() =>
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument(),
  );
  fireEvent.click(
    await screen.findByRole("button", {
      name: "Изменить: Использую PowerShell",
    }),
  );
  expect(screen.getByRole("combobox")).toHaveValue("powershell");
  fireEvent.click(screen.getByRole("button", { name: "Отмена" }));
});
it("shows an explicit replacement and reconciles a lost approval response", async () => {
  const { state } = fixture();
  state.entries = [entry("bash")];
  state.proposals = [{ ...proposal(), replaces: state.entries[0]! }];
  state.extraction = "ready";
  state.lose = true;
  mountPage();
  await screen.findByText("Заменит сохранённое предпочтение: Использую Bash");
  fireEvent.click(
    screen.getByRole("button", { name: "Запомнить: Использую PowerShell" }),
  );
  await waitFor(() => expect(state.proposals[0]?.state).toBe("accepted"));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Запомнить: Использую PowerShell" }),
    ).not.toBeInTheDocument(),
  );
  expect(state.writes).toBe(1);
});
it("supports retrying failed model selection without changing confirmed memory", async () => {
  const { state } = fixture();
  state.failSelect = true;
  mountSuggestions();
  await screen.findByText(
    "Не удалось подобрать факты. Память не изменена; можно продолжать разговор.",
  );
  expect(state.entries).toHaveLength(0);
  state.failSelect = false;
  fireEvent.click(
    screen.getByRole("button", { name: "Повторить подбор фактов" }),
  );
  await screen.findByRole("button", {
    name: "Запомнить: Использую PowerShell",
  });
  expect(state.selections).toBe(2);
});
it("reconciles dismissal after a lost response from the memory page", async () => {
  const { state } = fixture();
  state.proposals = [proposal()];
  state.extraction = "ready";
  state.lose = true;
  mountPage();
  fireEvent.click(await screen.findByRole("button", { name: "Не сохранять" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Не сохранять" }),
    ).not.toBeInTheDocument(),
  );
  expect(state.entries).toHaveLength(0);
});
it.each([401, 403, 404])(
  "removes private memory after %s followed by 503 and rejects late read generations",
  async (status) => {
    const { state } = fixture();
    state.entries = [entry()];
    const { queryClient } = mountPage();
    await screen.findByRole("heading", { name: "Использую PowerShell" });
    const scope = queryAccessSignal(queryClient, memoryKeys.workspace);
    state.failRead = status;
    await queryClient.invalidateQueries({ queryKey: memoryKeys.workspace });
    await waitFor(() =>
      expect(queryClient.getQueryData(memoryKeys.workspace)).toBeNull(),
    );
    expect(scope.aborted).toBe(true);
    state.failRead = 503;
    await queryClient.invalidateQueries({ queryKey: memoryKeys.workspace });
    expect(
      screen.queryByRole("heading", { name: "Использую PowerShell" }),
    ).not.toBeInTheDocument();
  },
);
it("cancels late mutations on logout and never fills the next account cache", async () => {
  const { state } = fixture();
  state.proposals = [proposal()];
  state.extraction = "ready";
  let release!: () => void;
  server.use(
    http.put(
      "*/api/v1/memory/proposals/:id",
      () =>
        new Promise<Response>((resolve) => {
          release = () =>
            resolve(
              HttpResponse.json({
                proposal: { ...proposal(), state: "accepted" },
                entry: entry(),
              }),
            );
        }),
    ),
  );
  const { queryClient } = mountSuggestions();
  fireEvent.click(
    await screen.findByRole("button", {
      name: "Запомнить: Использую PowerShell",
    }),
  );
  await waitFor(() => expect(release).toBeDefined());
  abortSessionRequests(queryClient);
  queryClient.clear();
  release();
  await new Promise((resolve) => setTimeout(resolve, 30));
  expect(queryClient.getQueryData(memoryKeys.workspace)).toBeUndefined();
  expect(state.entries).toHaveLength(0);
});
it("does not repopulate deleted memory through an older in-flight read", async () => {
  const { state, workspace } = fixture();
  state.entries = [entry()];
  const { queryClient } = mountPage();
  await screen.findByRole("heading", { name: "Использую PowerShell" });
  const old = workspace();
  let release!: () => void;
  server.use(
    http.get(
      "*/api/v1/memory",
      () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(HttpResponse.json(old));
        }),
    ),
  );
  const reloading = queryClient.invalidateQueries({
    queryKey: memoryKeys.workspace,
  });
  await waitFor(() => expect(release).toBeDefined());
  fireEvent.click(
    screen.getByRole("button", { name: "Удалить: Использую PowerShell" }),
  );
  state.failRead = 503;
  server.use(http.get("*/api/v1/memory", () => error(503)));
  fireEvent.click(screen.getByRole("button", { name: "Удалить из памяти" }));
  await waitFor(() => expect(state.entries).toHaveLength(0));
  await waitFor(() =>
    expect(queryClient.getQueryData(memoryKeys.workspace)).toBeNull(),
  );
  release();
  await reloading;
  expect(
    screen.queryByRole("heading", { name: "Использую PowerShell" }),
  ).not.toBeInTheDocument();
});
it("allows retrying a workspace load and displays missing current assignment and plan", async () => {
  fixture();
  let fail = true;
  server.use(
    http.get("*/api/v1/memory", () =>
      fail
        ? error(503)
        : HttpResponse.json({
            entries: [],
            pending: [],
            options: [option],
            context: {
              team: { id: "t", name: "Команда" },
              role: "intern",
              mentor: null,
              progress_percent: null,
              requests: [],
            },
            profile_markdown: "Нет сохранённых предпочтений.",
          }),
    ),
  );
  mountPage();
  await screen.findByRole("alert");
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
  await screen.findByText("Наставник: Не назначен");
  expect(screen.getByText("План ещё не создан")).toBeVisible();
});
it("does not invent an approval when reconciliation finds a different state", async () => {
  const { state } = fixture();
  state.proposals = [proposal()];
  state.extraction = "ready";
  server.use(
    http.put("*/api/v1/memory/proposals/:id", () => HttpResponse.error()),
  );
  mountSuggestions();
  fireEvent.click(
    await screen.findByRole("button", {
      name: "Запомнить: Использую PowerShell",
    }),
  );
  await screen.findByRole("alert");
  expect(state.entries).toHaveLength(0);
  expect(
    screen.getByRole("button", { name: "Запомнить: Использую PowerShell" }),
  ).toBeEnabled();
});

it.each([401, 403, 404])(
  "clears every memory copy after a denied mutation (%s), including subsequent 503",
  async (status) => {
    const { state } = fixture();
    state.entries = [entry()];
    state.failWrite = status;
    const { queryClient } = mountPage();
    await screen.findByRole("heading", { name: "Использую PowerShell" });
    queryClient.setQueryData(memoryKeys.message("source"), {
      state: "ready",
      items: [proposal()],
    });
    const oldRead = queryAccessSignal(queryClient, memoryKeys.workspace);
    fireEvent.click(
      screen.getByRole("button", { name: "Изменить: Использую PowerShell" }),
    );
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "bash" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Сохранить предпочтение" }),
    );
    await waitFor(() =>
      expect(queryClient.getQueryData(memoryKeys.workspace)).toBeNull(),
    );
    expect(queryClient.getQueryData(memoryKeys.message("source"))).toBeNull();
    expect(oldRead.aborted).toBe(true);
    state.failRead = 503;
    await queryClient.invalidateQueries({ queryKey: memoryKeys.workspace });
    expect(
      screen.queryByRole("heading", { name: "Использую PowerShell" }),
    ).not.toBeInTheDocument();
  },
);

it.each(["ready", "skipped"] as const)(
  "keeps the ordinary chat uncluttered when selection is %s with no facts",
  async (extraction) => {
    const { state } = fixture();
    state.extraction = extraction;
    const { queryClient } = mountSuggestions();
    await waitFor(() =>
      expect(
        queryClient.getQueryData(memoryKeys.message("source")),
      ).toMatchObject({ state: extraction, items: [] }),
    );
    expect(
      screen.queryByRole("region", { name: "Предложения персональной памяти" }),
    ).not.toBeInTheDocument();
  },
);

it("does not report deletion when an uncertain write is followed by an ordinary 404", async () => {
  const { state } = fixture();
  state.entries = [entry()];
  server.use(
    http.delete("*/api/v1/memory/entries/:id", () => HttpResponse.error()),
    http.get("*/api/v1/memory/entries/:id", () => error(404)),
  );
  const { queryClient } = mountPage();
  fireEvent.click(
    await screen.findByRole("button", {
      name: "Удалить: Использую PowerShell",
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Удалить из памяти" }));
  await waitFor(() =>
    expect(queryClient.getQueryData(memoryKeys.workspace)).toBeNull(),
  );
  expect(state.entries).toHaveLength(1);
  expect(
    screen.queryByText(
      "Вы ещё ничего не сохранили. Предложения становятся памятью только после вашего подтверждения.",
    ),
  ).not.toBeInTheDocument();
});
