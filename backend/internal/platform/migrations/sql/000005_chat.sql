CREATE TABLE conversations (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    team_id text NOT NULL,
    client_conversation_id uuid NOT NULL,
    step_id text REFERENCES onboarding_steps(id),
    title text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    next_sequence bigint NOT NULL DEFAULT 1,
    FOREIGN KEY (user_id, team_id) REFERENCES users(id, team_id),
    UNIQUE (user_id, client_conversation_id)
);
CREATE TABLE chat_messages (
    id text PRIMARY KEY,
    conversation_id text NOT NULL REFERENCES conversations(id),
    sequence bigint NOT NULL,
    client_message_id uuid,
    role text NOT NULL CHECK (role IN ('user','assistant')),
    text text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('generating','completed','failed','interrupted')),
    in_reply_to_message_id text,
    sources jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(sources)='array'),
    error jsonb,
    trace_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, sequence),
    UNIQUE (conversation_id, client_message_id),
    UNIQUE (conversation_id, id),
    FOREIGN KEY (conversation_id,in_reply_to_message_id) REFERENCES chat_messages(conversation_id,id),
    CHECK ((role='user' AND status='completed' AND client_message_id IS NOT NULL AND in_reply_to_message_id IS NULL)
        OR (role='assistant' AND client_message_id IS NULL AND in_reply_to_message_id IS NOT NULL)),
    CHECK ((status IN ('failed','interrupted') AND error IS NOT NULL AND jsonb_typeof(error)='object')
        OR (status NOT IN ('failed','interrupted') AND error IS NULL))
);
CREATE UNIQUE INDEX chat_one_generation ON chat_messages(conversation_id) WHERE status='generating';
CREATE UNIQUE INDEX chat_one_reply ON chat_messages(in_reply_to_message_id) WHERE role='assistant';
CREATE INDEX conversations_owner_updated ON conversations(user_id,team_id,updated_at DESC,id);
-- #14 owns the request records. Its SQL store links a verified request in the
-- same transaction, then a later migration may add the request-side foreign key.
CREATE TABLE chat_message_requests (
    assistant_message_id text NOT NULL REFERENCES chat_messages(id),
    request_id text NOT NULL,
    PRIMARY KEY (assistant_message_id,request_id)
);
