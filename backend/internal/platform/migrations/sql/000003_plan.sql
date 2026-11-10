CREATE TABLE onboarding_plans (
    id text PRIMARY KEY,
    user_id text NOT NULL UNIQUE,
    team_id text NOT NULL,
    title text NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    FOREIGN KEY (user_id, team_id) REFERENCES users(id, team_id)
);
CREATE TABLE onboarding_steps (
    id text PRIMARY KEY,
    plan_id text NOT NULL REFERENCES onboarding_plans(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    title text NOT NULL,
    description text NOT NULL,
    status text NOT NULL CHECK (status IN ('not_started', 'in_progress', 'blocked', 'completed')),
    blocked_reason text,
    resource_id text,
    permission text,
    UNIQUE (plan_id, position),
    UNIQUE (plan_id, id),
    CHECK ((status = 'blocked' AND blocked_reason IS NOT NULL AND length(btrim(blocked_reason)) > 0) OR
           (status <> 'blocked' AND blocked_reason IS NULL)),
    CHECK ((resource_id IS NULL AND permission IS NULL) OR
           (resource_id IS NOT NULL AND permission IS NOT NULL))
);
CREATE TABLE onboarding_step_dependencies (
    plan_id text NOT NULL,
    step_id text NOT NULL,
    prerequisite_id text NOT NULL,
    PRIMARY KEY (step_id, prerequisite_id),
    FOREIGN KEY (plan_id, step_id) REFERENCES onboarding_steps(plan_id, id) ON DELETE CASCADE,
    FOREIGN KEY (plan_id, prerequisite_id) REFERENCES onboarding_steps(plan_id, id),
    CHECK (step_id <> prerequisite_id)
);
-- Request-service owns request records in #14. These are plan-side links/snapshots;
-- every write goes through onboarding.ApplyRequestStateTx and locks the parent plan.
CREATE TABLE onboarding_step_requests (
    step_id text NOT NULL REFERENCES onboarding_steps(id) ON DELETE CASCADE,
    request_id text NOT NULL,
    resource_id text NOT NULL,
    permission text NOT NULL,
    status text NOT NULL CHECK (status IN ('draft','submitting','pending','granted','rejected','failed')),
    PRIMARY KEY (step_id, request_id)
);
