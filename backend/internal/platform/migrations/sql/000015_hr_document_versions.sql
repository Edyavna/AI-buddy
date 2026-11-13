ALTER TABLE knowledge_documents
    ADD COLUMN direction text NOT NULL DEFAULT 'mentor' CHECK (direction IN ('mentor','hr')),
    ADD COLUMN version bigint NOT NULL DEFAULT 1;

CREATE TABLE hr_documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id text NOT NULL REFERENCES users(id),
    create_id uuid NOT NULL,
    create_hash text NOT NULL,
    latest_version bigint NOT NULL DEFAULT 1,
    published_version bigint,
    revoked boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(author_id,create_id)
);
CREATE TABLE hr_document_versions (
    document_id uuid NOT NULL REFERENCES hr_documents(id),
    version bigint NOT NULL,
    title text NOT NULL,
    content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 1048576),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','indexing','ready','failed')),
    publisher_id text REFERENCES users(id),
    claim uuid,
    lease_until timestamptz,
    error text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(document_id,version)
);
CREATE TABLE hr_document_teams (
    document_id uuid NOT NULL,
    version bigint NOT NULL,
    team_id text NOT NULL REFERENCES teams(id),
    PRIMARY KEY(document_id,version,team_id),
    FOREIGN KEY(document_id,version) REFERENCES hr_document_versions(document_id,version)
);
CREATE INDEX hr_document_jobs ON hr_document_versions(updated_at,document_id) WHERE status='indexing';
CREATE TRIGGER hr_document_access_changed AFTER UPDATE OF revoked,published_version ON hr_documents
    FOR EACH ROW WHEN (OLD.published_version IS NOT NULL AND (OLD.revoked IS DISTINCT FROM NEW.revoked OR OLD.published_version IS DISTINCT FROM NEW.published_version))
    EXECUTE FUNCTION change_access_scope();

ALTER TABLE chat_messages ADD COLUMN routing jsonb;
