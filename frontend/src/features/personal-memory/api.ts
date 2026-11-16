import { z } from "zod";
import { apiRequest, ClientError } from "@/shared/api";
import type { components } from "@/shared/api/generated/schema";

type Schemas = components["schemas"];
const option = {
  category: z.enum(["work_environment", "learning", "instruction_format"]),
  key: z.enum(["os", "shell", "technology", "format"]),
  value: z.string(),
  text: z.string(),
};
export const entrySchema = z.object({
  ...option,
  id: z.string(),
  source_message_id: z.string(),
  conversation_id: z.string(),
  revision: z.number().int().positive(),
  edited: z.boolean(),
  created_at: z.iso.datetime(),
  updated_at: z.iso.datetime(),
}) satisfies z.ZodType<Schemas["MemoryEntry"]>;
export const proposalSchema = z.object({
  ...option,
  id: z.string(),
  source_message_id: z.string(),
  conversation_id: z.string(),
  state: z.enum(["pending", "accepted", "dismissed"]),
  created_at: z.iso.datetime(),
  entry_id: z.string().nullable(),
  replaces: entrySchema.nullable(),
  uncertain: z.boolean().exactOptional(),
}) satisfies z.ZodType<Schemas["MemoryProposal"]>;
export const proposalsSchema = z.object({
  state: z.enum(["not_requested", "ready", "failed", "skipped"]),
  items: z.array(proposalSchema),
}) satisfies z.ZodType<Schemas["MemoryProposals"]>;
export const workspaceSchema = z.object({
  entries: z.array(entrySchema),
  pending: z.array(proposalSchema),
  options: z.array(z.object(option)),
  context: z.object({
    team: z.object({ id: z.string(), name: z.string() }),
    role: z.literal("intern"),
    mentor: z.object({ id: z.string(), name: z.string() }).nullable(),
    progress_percent: z.number().int().min(0).max(100).nullable(),
    requests: z.array(
      z.object({ status: z.string(), count: z.number().int().nonnegative() }),
    ),
    recent_requests: z
      .array(
        z.object({
          id: z.string(),
          resource_name: z.string(),
          permission: z.enum(["read", "write"]),
          status: z.enum([
            "draft",
            "submitting",
            "pending",
            "granted",
            "rejected",
            "failed",
          ]),
        }),
      )
      .default([]),
  }),
  profile_markdown: z.string().max(4096),
}) satisfies z.ZodType<Schemas["MemoryWorkspace"]>;
export type Entry = z.infer<typeof entrySchema>;
export type Proposal = z.infer<typeof proposalSchema>;
export type Option = Schemas["MemoryOption"];
export const memoryKeys = {
  all: ["personal-memory"] as const,
  workspace: ["personal-memory", "workspace"] as const,
  message: (id: string) => ["personal-memory", "message", id] as const,
};
async function request<T>(
  path: string,
  schema: z.ZodType<T>,
  signal: AbortSignal,
  method: "GET" | "POST" | "PUT" | "DELETE" = "GET",
  body?: unknown,
): Promise<T> {
  const scope = AbortSignal.any([signal, AbortSignal.timeout(8000)]);
  const result = await apiRequest(path, schema, {
    method,
    signal: scope,
    ...(body !== undefined ? { body } : {}),
  });
  scope.throwIfAborted();
  if (!result.data)
    throw new ClientError("protocol", "Ответ памяти не получен");
  return result.data;
}
const path = (id: string) =>
  `/messages/${encodeURIComponent(id)}/memory-proposals`;
export const memoryApi = {
  workspace: (signal: AbortSignal) =>
    request("/memory", workspaceSchema, signal),
  proposals: (id: string, signal: AbortSignal) =>
    request(path(id), proposalsSchema, signal),
  suggest: (id: string, signal: AbortSignal) =>
    request(path(id), proposalsSchema, signal, "POST", {}),
  decide: (
    proposal: Proposal,
    decision: "accept" | "dismiss",
    signal: AbortSignal,
  ) =>
    request(
      `/memory/proposals/${encodeURIComponent(proposal.id)}`,
      z.object({ proposal: proposalSchema, entry: entrySchema.nullable() }),
      signal,
      "PUT",
      {
        decision,
        expected_entry_id: proposal.replaces?.id ?? null,
        expected_revision: proposal.replaces?.revision ?? 0,
      },
    ),
  edit: (id: string, value: string, revision: number, signal: AbortSignal) =>
    request(
      `/memory/entries/${encodeURIComponent(id)}`,
      entrySchema,
      signal,
      "PUT",
      { value, revision },
    ),
  remove: (id: string, revision: number, signal: AbortSignal) =>
    request(
      `/memory/entries/${encodeURIComponent(id)}`,
      z.object({ deleted: z.literal(true) }),
      signal,
      "DELETE",
      { revision },
    ),
  entry: (id: string, signal: AbortSignal) =>
    request(`/memory/entries/${encodeURIComponent(id)}`, entrySchema, signal),
};
