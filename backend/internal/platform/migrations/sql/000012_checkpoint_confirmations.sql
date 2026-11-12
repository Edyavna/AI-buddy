ALTER TABLE onboarding_steps
    ADD COLUMN template_key text CHECK (template_key IS NULL OR length(btrim(template_key)) > 0),
    ADD COLUMN period text,
    ADD COLUMN completion_policy text NOT NULL DEFAULT 'intern'
        CHECK (completion_policy IN ('intern', 'mentor', 'both')),
    ADD COLUMN instructions text,
    ADD COLUMN resources jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(resources) = 'array');

-- Legacy inserts without a catalog key retain the step ID as their effective key.
CREATE UNIQUE INDEX onboarding_steps_template_key
    ON onboarding_steps (plan_id, COALESCE(template_key, id));

CREATE TABLE onboarding_step_confirmations (
    step_id text NOT NULL REFERENCES onboarding_steps(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('intern', 'mentor')),
    user_id text NOT NULL CHECK (length(user_id) > 0),
    confirmed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (step_id, role)
);

-- The legacy status did not store a completion timestamp.
INSERT INTO onboarding_step_confirmations(step_id, role, user_id, confirmed_at)
SELECT s.id, 'intern', p.user_id, transaction_timestamp()
FROM onboarding_steps s JOIN onboarding_plans p ON p.id = s.plan_id
WHERE s.status = 'completed';
