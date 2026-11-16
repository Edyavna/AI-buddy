ALTER TABLE memory_proposals ADD COLUMN uncertain boolean NOT NULL DEFAULT false;

-- A follow-up can display the proposals belonging to its preceding user turn.
ALTER TABLE memory_extractions ADD COLUMN source_message_id text REFERENCES chat_messages(id) ON DELETE CASCADE;
CREATE INDEX memory_extractions_source ON memory_extractions(source_message_id) WHERE source_message_id IS NOT NULL;
