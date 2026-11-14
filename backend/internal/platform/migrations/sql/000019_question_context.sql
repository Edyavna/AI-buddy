ALTER TABLE knowledge_gaps ADD COLUMN context_snapshot jsonb;
ALTER TABLE question_drafts ADD COLUMN context_snapshot jsonb;

-- A successful, authorized document fetch from a cited chat message. No body or
-- title is copied here; a later preview rechecks the current document/version.
CREATE TABLE question_document_opens (
    student_id text NOT NULL REFERENCES users(id),
    message_id text NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
    document_id text NOT NULL,
    version bigint NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(student_id,message_id,document_id,version)
);
