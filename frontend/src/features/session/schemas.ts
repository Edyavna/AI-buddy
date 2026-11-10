import { z } from 'zod';

// Runtime boundary schemas for session endpoints (OpenAPI is the source of truth).
export const userSchema = z.object({
  id: z.string(),
  name: z.string(),
  role: z.enum(['intern', 'mentor']),
  team: z.object({ id: z.string(), name: z.string() }),
});

export const demoAccountsSchema = z.array(userSchema);

export const createDemoSessionSchema = z
  .object({ account_id: z.string() })
  .strict();
