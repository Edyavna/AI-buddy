CREATE TABLE mentor_documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id text NOT NULL,
    team_id text NOT NULL,
    upload_id uuid NOT NULL,
    question_id uuid REFERENCES knowledge_gaps(id),
    title text NOT NULL,
    content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 1048576),
    status text NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'ready', 'failed')),
    error text,
    claim uuid,
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (author_id, team_id) REFERENCES users(id, team_id),
    UNIQUE (author_id, upload_id)
);

CREATE INDEX mentor_documents_author ON mentor_documents (team_id, author_id, created_at DESC);
CREATE INDEX mentor_documents_pending ON mentor_documents (updated_at, id) WHERE status = 'processing';
