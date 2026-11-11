CREATE TABLE knowledge_gaps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id text NOT NULL UNIQUE REFERENCES chat_messages(id),
    student_id text NOT NULL,
    team_id text NOT NULL,
    mentor_id text NOT NULL,
    step_id text REFERENCES onboarding_steps(id),
    question text NOT NULL,
    context_excerpt text NOT NULL,
    status text NOT NULL DEFAULT 'new' CHECK (status IN ('new','in_progress','resolved')),
    revision bigint NOT NULL DEFAULT 1,
    resolution text,
    document_ids jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    FOREIGN KEY (student_id, team_id) REFERENCES users(id, team_id),
    FOREIGN KEY (mentor_id, team_id) REFERENCES users(id, team_id)
);
CREATE INDEX knowledge_gaps_queue ON knowledge_gaps (mentor_id, team_id, status, created_at);
CREATE TABLE knowledge_gap_history (
    question_id uuid NOT NULL REFERENCES knowledge_gaps(id),
    revision bigint NOT NULL,
    snapshot jsonb NOT NULL,
    PRIMARY KEY (question_id, revision)
);
