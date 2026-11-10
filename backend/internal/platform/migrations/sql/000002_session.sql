CREATE TABLE teams (
    id text PRIMARY KEY,
    name text NOT NULL
);
CREATE TABLE users (
    id text PRIMARY KEY,
    team_id text NOT NULL REFERENCES teams(id),
    name text NOT NULL,
    role text NOT NULL CHECK (role IN ('intern', 'mentor')),
    demo_account boolean NOT NULL DEFAULT false,
    UNIQUE (id, team_id)
);
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);
