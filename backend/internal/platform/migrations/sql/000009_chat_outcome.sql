ALTER TABLE chat_messages ADD COLUMN outcome text;

ALTER TABLE chat_messages ADD CONSTRAINT chat_messages_outcome
    CHECK (outcome IS NULL OR (role='assistant' AND status='completed'
        AND outcome IN ('answered','needs_clarification','knowledge_gap')));
