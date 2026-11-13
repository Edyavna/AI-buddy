CREATE TABLE question_groups (
    id uuid PRIMARY KEY,
    author_id text NOT NULL REFERENCES users(id),
    direction text NOT NULL CHECK(direction IN ('mentor','hr')),
    team_id text NOT NULL REFERENCES teams(id),
    title text NOT NULL,
    resolution text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1 CHECK(revision > 0),
    create_hash text NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX question_groups_owner ON question_groups(author_id,direction,updated_at DESC);
CREATE TABLE question_group_members (
    group_id uuid NOT NULL REFERENCES question_groups(id),
    question_id uuid NOT NULL REFERENCES knowledge_gaps(id),
    question_revision bigint NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivered','closed','failed')),
    error text,
    removed_at timestamptz,
    PRIMARY KEY(group_id,question_id)
);
CREATE UNIQUE INDEX question_group_active_member ON question_group_members(question_id) WHERE removed_at IS NULL;
CREATE TABLE question_group_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id uuid NOT NULL REFERENCES question_groups(id),
    author_id text NOT NULL REFERENCES users(id),
    event text NOT NULL,
    question_ids jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- A separate source store reuses #56 without inventing personal questions or
-- marking participants resolved before explicit delivery and closure.
CREATE TABLE resolution_group_drafts (
    group_id uuid NOT NULL REFERENCES question_groups(id),
    author_id text NOT NULL REFERENCES users(id),
    revision bigint NOT NULL CHECK(revision > 0),
    payload jsonb NOT NULL,
    document_id uuid,
    document_version bigint,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(group_id,author_id)
);
CREATE INDEX resolution_group_publications ON resolution_group_drafts(document_id,document_version);
