import { z } from "zod";
import { apiRequest, ClientError } from "@/shared/api";
import type { components } from "@/shared/api/generated/schema";

const state = z.enum(["available", "unavailable"]);
export const contextInputSchema = z.object({
  reviewed: z.boolean(),
  include_step: z.boolean(),
  step_revision: z.number().int().positive().nullable(),
  attempts: z.string(),
  fragments: z.array(z.object({ message_id: z.string(), text: z.string() })),
  documents: z.array(
    z.object({ document_id: z.string(), version: z.number().int().positive() }),
  ),
}) satisfies z.ZodType<components["schemas"]["QuestionContextInput"]>;
export const contextSchema = z.object({
  conversation_id: z.string(),
  attempts: z.string(),
  step: z
    .object({
      id: z.string(),
      title: z.string().nullable(),
      status: state,
      revision: z.number().int().nonnegative(),
    })
    .nullable(),
  fragments: z.array(
    z.object({
      message_id: z.string(),
      role: z.enum(["user", "assistant"]),
      text: z.string().nullable(),
      edited: z.boolean(),
      status: state,
    }),
  ),
  documents: z.array(
    z.object({
      document_id: z.string(),
      version: z.number().int().positive(),
      title: z.string().nullable(),
      opened: z.boolean(),
      status: state,
    }),
  ),
  limited: z.boolean(),
}) satisfies z.ZodType<components["schemas"]["QuestionContextSnapshot"]>;
export type ContextInput = components["schemas"]["QuestionContextInput"];
export type ContextSnapshot = z.infer<typeof contextSchema>;
export const emptyContext = (): ContextInput => ({
  reviewed: false,
  include_step: false,
  step_revision: null,
  attempts: "",
  fragments: [],
  documents: [],
});
export const contextKey = (id: string, direction: string) =>
  ["question-context", id, direction] as const;
export async function previewContext(
  id: string,
  direction: string,
  signal: AbortSignal,
) {
  const scope = AbortSignal.any([signal, AbortSignal.timeout(10000)]);
  const result = await apiRequest(
    `/messages/${encodeURIComponent(id)}/question-context`,
    contextSchema,
    { search: { direction }, signal: scope },
  );
  scope.throwIfAborted();
  if (!result.data) throw new ClientError("protocol", "Контекст не получен");
  return result.data;
}
export function sameContext(
  snapshot: ContextSnapshot | null | undefined,
  input: ContextInput | null | undefined,
) {
  if (!input) return !snapshot;
  return (
    !!snapshot &&
    snapshot.attempts === input.attempts &&
    !!snapshot.step === input.include_step &&
    (!input.include_step || snapshot.step?.revision === input.step_revision) &&
    JSON.stringify(
      snapshot.fragments.map((f) => ({
        message_id: f.message_id,
        text: f.text,
      })),
    ) === JSON.stringify(input.fragments) &&
    JSON.stringify(
      snapshot.documents.map((d) => ({
        document_id: d.document_id,
        version: d.version,
      })),
    ) === JSON.stringify(input.documents)
  );
}
