CREATE TABLE playbook_documents (
    document_id text PRIMARY KEY REFERENCES knowledge_documents(id),
    path text NOT NULL,
    part integer NOT NULL CHECK (part > 0),
    commit_sha text NOT NULL,
    blob_sha text NOT NULL,
    original_url text NOT NULL,
    active boolean NOT NULL
);

CREATE UNIQUE INDEX playbook_documents_active_part ON playbook_documents(path, part) WHERE active;

CREATE TABLE playbook_index (
    singleton boolean PRIMARY KEY CHECK (singleton),
    generation bigint NOT NULL DEFAULT 0,
    commit_sha text NOT NULL DEFAULT ''
);

INSERT INTO playbook_index(singleton) VALUES(true);
