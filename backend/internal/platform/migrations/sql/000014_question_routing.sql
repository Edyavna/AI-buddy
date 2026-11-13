ALTER TABLE knowledge_gaps
    ALTER COLUMN mentor_id DROP NOT NULL,
    DROP CONSTRAINT knowledge_gaps_message_id_key,
    ADD COLUMN direction text NOT NULL DEFAULT 'mentor' CHECK (direction IN ('mentor','hr')),
    ADD COLUMN original_direction text NOT NULL DEFAULT 'mentor' CHECK (original_direction IN ('mentor','hr')),
    ADD COLUMN hr_id text REFERENCES users(id),
    ADD COLUMN routing jsonb,
    ADD CONSTRAINT knowledge_gaps_message_route_key UNIQUE (message_id, original_direction),
    ADD CONSTRAINT knowledge_gaps_specialist_check CHECK (
        (direction='mentor' AND mentor_id IS NOT NULL AND hr_id IS NULL) OR
        (direction='hr' AND mentor_id IS NULL)
    );
CREATE INDEX knowledge_gaps_hr_queue ON knowledge_gaps(team_id,status,created_at) WHERE direction='hr';
CREATE TABLE question_drafts (
    message_id text NOT NULL REFERENCES chat_messages(id),
    direction text NOT NULL CHECK (direction IN ('mentor','hr')),
    question text NOT NULL,
    context_excerpt text NOT NULL,
    PRIMARY KEY (message_id,direction)
);
CREATE TABLE question_operations (
    actor_id text NOT NULL REFERENCES users(id),
    operation_id uuid NOT NULL,
    payload_hash text NOT NULL,
    question_id uuid NOT NULL REFERENCES knowledge_gaps(id),
    applied_revision bigint NOT NULL,
    PRIMARY KEY(actor_id,operation_id)
);
CREATE TRIGGER question_access_changed AFTER UPDATE OF direction ON knowledge_gaps
    FOR EACH ROW WHEN (OLD.direction IS DISTINCT FROM NEW.direction) EXECUTE FUNCTION change_access_scope();
