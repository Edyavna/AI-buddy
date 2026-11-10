CREATE SCHEMA demo_external;

CREATE TABLE demo_external.resources (
    id text PRIMARY KEY,
    team_id text NOT NULL,
    name text NOT NULL,
    permission text NOT NULL,
    external_id text NOT NULL,
    external_role text NOT NULL,
    UNIQUE (id, team_id, permission)
);
CREATE TABLE demo_external.tasks (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    team_id text NOT NULL,
    resource_id text NOT NULL REFERENCES demo_external.resources(id),
    position integer NOT NULL DEFAULT 0,
    title text NOT NULL,
    description text NOT NULL,
    url text NOT NULL DEFAULT ''
);
CREATE TABLE demo_external.access_requests (
    client_reference text PRIMARY KEY,
    external_id text NOT NULL UNIQUE,
    user_id text NOT NULL,
    team_id text NOT NULL,
    resource_id text NOT NULL,
    permission text NOT NULL,
    resource_name text NOT NULL,
    reason text NOT NULL,
    external_resource_id text NOT NULL,
    external_role text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','granted','rejected')),
    failure_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (resource_id, team_id, permission) REFERENCES demo_external.resources(id, team_id, permission),
    CHECK ((status = 'rejected') = (length(btrim(failure_reason)) > 0))
);
CREATE TABLE demo_external.grants (
    user_id text NOT NULL,
    team_id text NOT NULL,
    resource_id text NOT NULL,
    permission text NOT NULL,
    PRIMARY KEY (user_id, team_id, resource_id, permission),
    FOREIGN KEY (resource_id, team_id, permission) REFERENCES demo_external.resources(id, team_id, permission)
);
CREATE TABLE demo_external.faults (
    client_reference text PRIMARY KEY,
    mode text NOT NULL CHECK (mode IN ('reject-before-write','timeout-after-commit','lookup-unavailable'))
);
