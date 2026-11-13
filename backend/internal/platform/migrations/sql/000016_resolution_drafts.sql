ALTER TABLE mentor_documents ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
CREATE TABLE resolution_drafts (
    question_id uuid NOT NULL REFERENCES knowledge_gaps(id),
    author_id text NOT NULL REFERENCES users(id),
    revision bigint NOT NULL CHECK (revision > 0),
    payload jsonb NOT NULL,
    document_id uuid,
    document_version bigint,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (question_id, author_id)
);
CREATE INDEX resolution_publications ON resolution_drafts(document_id,document_version);
CREATE TABLE mentor_document_versions (
    document_id uuid NOT NULL REFERENCES mentor_documents(id),
    version bigint NOT NULL,
    title text NOT NULL,
    content text NOT NULL,
    PRIMARY KEY(document_id,version)
);
INSERT INTO mentor_document_versions SELECT id,version,title,content FROM mentor_documents;
