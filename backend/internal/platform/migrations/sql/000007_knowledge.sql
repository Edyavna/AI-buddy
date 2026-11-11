CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE knowledge_documents (
    id text PRIMARY KEY,
    source text NOT NULL,
    source_id text NOT NULL,
    title text NOT NULL,
    content text NOT NULL,
    updated_at timestamptz NOT NULL,
    team_id text NOT NULL,
    users jsonb NOT NULL,
    hash text NOT NULL,
    UNIQUE(source,source_id)
);

CREATE TABLE knowledge_chunks (
    id text PRIMARY KEY,
    document_id text NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
    excerpt text NOT NULL,
    embedding vector(1024) NOT NULL
);
CREATE INDEX knowledge_documents_team ON knowledge_documents(team_id);
CREATE INDEX knowledge_chunks_document ON knowledge_chunks(document_id);
