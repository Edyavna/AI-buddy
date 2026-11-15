CREATE TABLE memory_extractions (
    message_id text PRIMARY KEY REFERENCES chat_messages(id) ON DELETE CASCADE,
    user_id text NOT NULL,
    team_id text NOT NULL,
    state text NOT NULL CHECK (state IN ('ready','failed','skipped')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id,team_id) REFERENCES users(id,team_id) ON DELETE CASCADE
);
CREATE TABLE memory_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id text NOT NULL,
    team_id text NOT NULL,
    slot text NOT NULL,
    category text NOT NULL CHECK (category IN ('work_environment','learning','instruction_format')),
    fact_key text NOT NULL CHECK (fact_key IN ('os','shell','technology','format')),
    value text NOT NULL CHECK (length(value) BETWEEN 1 AND 40),
    source_message_id text NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    edited boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id,team_id) REFERENCES users(id,team_id) ON DELETE CASCADE,
    UNIQUE(user_id,team_id,slot)
);
CREATE TABLE memory_proposals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id text NOT NULL,
    team_id text NOT NULL,
    source_message_id text NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
    category text NOT NULL CHECK (category IN ('work_environment','learning','instruction_format')),
    fact_key text NOT NULL CHECK (fact_key IN ('os','shell','technology','format')),
    value text NOT NULL CHECK (length(value) BETWEEN 1 AND 40),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','accepted','dismissed')),
    entry_id uuid REFERENCES memory_entries(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id,team_id) REFERENCES users(id,team_id) ON DELETE CASCADE,
    UNIQUE(user_id,team_id,source_message_id,fact_key,value)
);
CREATE INDEX memory_proposals_owner ON memory_proposals(user_id,team_id,state);
-- Deletion receipts retain no preference value or source text. They distinguish
-- an acknowledged deletion from an ordinary access-denied/not-found response.
CREATE TABLE memory_deletions (
    entry_id uuid PRIMARY KEY,
    user_id text NOT NULL,
    team_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    deleted_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id,team_id) REFERENCES users(id,team_id) ON DELETE CASCADE
);
