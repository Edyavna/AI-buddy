CREATE TABLE access_requests (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    team_id text NOT NULL,
    conversation_id text NOT NULL,
    assistant_message_id text NOT NULL,
    step_id text REFERENCES onboarding_steps(id),
    resource_id text NOT NULL,
    resource_name text NOT NULL,
    permission text NOT NULL CHECK (permission IN ('read','write')),
    reason text NOT NULL,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','submitting','pending','granted','rejected','failed')),
    external_id text,
    status_checked_at timestamptz,
    failure_reason text NOT NULL DEFAULT '',
    draft_version bigint NOT NULL DEFAULT 1 CHECK (draft_version > 0),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision >= draft_version),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id,team_id) REFERENCES users(id,team_id),
    FOREIGN KEY (conversation_id,assistant_message_id) REFERENCES chat_messages(conversation_id,id),
    CHECK (CASE
        WHEN status IN ('pending','granted','rejected') THEN external_id IS NOT NULL AND external_id <> ''
        WHEN status IN ('draft','failed') THEN external_id IS NULL
        ELSE true
    END),
    CHECK (status NOT IN ('failed','rejected') OR length(btrim(failure_reason)) > 0)
);

CREATE UNIQUE INDEX access_requests_one_active
    ON access_requests(user_id,team_id,resource_id,permission)
    WHERE status IN ('draft','submitting','pending','granted');
CREATE INDEX access_requests_owner_created
    ON access_requests(user_id,team_id,created_at DESC,id);

CREATE TABLE access_request_jobs (
    request_id text PRIMARY KEY REFERENCES access_requests(id) ON DELETE CASCADE,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    attempt bigint NOT NULL DEFAULT 0
);
CREATE INDEX access_request_jobs_due ON access_request_jobs(next_attempt_at,request_id);
