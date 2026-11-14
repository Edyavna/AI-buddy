ALTER TABLE onboarding_steps
    ADD COLUMN blocked_at timestamptz;

CREATE TABLE onboarding_step_blocking_history (
    id text PRIMARY KEY,
    step_id text NOT NULL REFERENCES onboarding_steps(id) ON DELETE CASCADE,
    blocked_reason text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ended_at timestamptz
);

CREATE INDEX onboarding_step_blocking_history_step_idx
    ON onboarding_step_blocking_history (step_id, started_at);
