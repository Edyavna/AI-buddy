ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('intern', 'mentor', 'hr'));

CREATE TABLE hr_team_assignments (
    hr_user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id text NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (hr_user_id, team_id)
);
CREATE INDEX hr_team_active ON hr_team_assignments (team_id, hr_user_id) WHERE active;

-- Version changes invalidate cached private data across browser sessions.
CREATE SEQUENCE access_scope_version;
CREATE TABLE access_scope (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL DEFAULT 0
);
INSERT INTO access_scope DEFAULT VALUES;
CREATE FUNCTION change_access_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE access_scope SET version=nextval('access_scope_version');
    RETURN NULL;
END;
$$;
CREATE TRIGGER hr_scope_changed AFTER INSERT OR UPDATE OR DELETE ON hr_team_assignments
    FOR EACH ROW EXECUTE FUNCTION change_access_scope();
CREATE TRIGGER mentor_scope_changed AFTER INSERT OR UPDATE OR DELETE ON mentor_assignments
    FOR EACH ROW EXECUTE FUNCTION change_access_scope();
