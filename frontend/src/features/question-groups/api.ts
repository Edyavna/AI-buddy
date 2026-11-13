import { z } from "zod";
import { apiRequest, ClientError, type RequestOptions } from "@/shared/api";
import {
  resolutionDraftSchema,
  type Direction,
} from "@/features/resolution-drafts/api";
import type { components } from "@/shared/api/generated/schema";

export const groupQuestionSchema = z.object({
  id: z.string(),
  team_id: z.string(),
  question: z.string(),
  student_id: z.string(),
  student_name: z.string(),
  step_id: z.string().nullable(),
  revision: z.number().int().positive(),
  status: z.enum(["new", "in_progress", "resolved"]),
  created_at: z.string(),
});
export const groupSchema = z.object({
  id: z.string(),
  author_id: z.string(),
  direction: z.enum(["mentor", "hr"]),
  team_id: z.string(),
  title: z.string(),
  resolution: z.string(),
  revision: z.number().int().positive(),
  published_at: z.string().nullable(),
  members: z.array(
    z.object({
      id: z.string(),
      question: groupQuestionSchema.nullable(),
      status: z.enum(["pending", "delivered", "closed", "failed"]),
      error: z.string().nullable(),
    }),
  ),
  history: z.array(
    z.object({
      event: z.string(),
      question_ids: z.array(z.string()),
      created_at: z.string(),
    }),
  ),
  draft: resolutionDraftSchema.nullable(),
  publication_error: z.string().nullable().optional(),
  delivered_count: z.number().int().nonnegative(),
  repeat_count: z.number().int().nonnegative(),
  repeat_count_limited: z.boolean(),
});
export const groupWorkspaceSchema = z.object({
  groups: z.array(groupSchema),
  candidates: z.array(groupQuestionSchema),
  suggestions: z.array(
    z.object({
      question_ids: z.array(z.string()),
      reason: z.string(),
      score: z.number(),
    }),
  ),
  limited: z.boolean(),
});
export type Group = z.infer<typeof groupSchema>;
export type GroupWorkspace = z.infer<typeof groupWorkspaceSchema>;
export type GroupInput = components["schemas"]["SaveQuestionGroup"];
export const groupKeys = {
  workspace: (d: Direction) => ["question-groups", d] as const,
  detail: (d: Direction, id: string) => ["question-groups", d, id] as const,
};
const root = (direction: Direction, id = "") =>
  `/${direction}/question-groups${id ? "/" + encodeURIComponent(id) : ""}`;
async function request<T>(
  path: string,
  schema: z.ZodType<T>,
  options: RequestOptions,
) {
  const result = await apiRequest(path, schema, options);
  options.signal?.throwIfAborted();
  if (!result.data)
    throw new ClientError("protocol", "Сервер вернул пустой ответ");
  return result.data;
}
export const groupApi = {
  workspace: (d: Direction, signal: AbortSignal) =>
    request(root(d), groupWorkspaceSchema, { signal }),
  detail: (d: Direction, id: string, signal: AbortSignal) =>
    request(root(d, id), groupSchema, { signal }),
  save: (d: Direction, id: string, body: GroupInput, signal: AbortSignal) =>
    request(root(d, id), groupSchema, {
      method: id ? "PUT" : "POST",
      body,
      signal,
    }),
  deliver: (
    d: Direction,
    id: string,
    revision: number,
    close: boolean,
    signal: AbortSignal,
  ) =>
    request(root(d, id) + "/" + (close ? "close" : "deliver"), groupSchema, {
      method: "POST",
      body: { revision },
      signal,
    }),
};
