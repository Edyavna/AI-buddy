CREATE TABLE curator_team_assignments (
    curator_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id text NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    PRIMARY KEY(curator_id,team_id)
);
CREATE TRIGGER curator_scope_changed AFTER INSERT OR UPDATE OR DELETE ON curator_team_assignments
    FOR EACH ROW EXECUTE FUNCTION change_access_scope();
CREATE TABLE onboarding_system_problems (
    id uuid PRIMARY KEY,
    team_id text NOT NULL REFERENCES teams(id),
    kind text NOT NULL CHECK (kind IN ('access','blocked','knowledge')),
    direction text NOT NULL CHECK (direction IN ('mentor','hr')),
    title text NOT NULL,
    description text NOT NULL,
    status text NOT NULL CHECK (status IN ('open','in_progress','resolved','merged')),
    owner_id text REFERENCES users(id),
    due_at timestamptz,
    resolution text NOT NULL DEFAULT '',
    document_id text REFERENCES knowledge_documents(id),
    template_version bigint,
    merged_into uuid REFERENCES onboarding_system_problems(id),
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX onboarding_system_problems_team ON onboarding_system_problems(team_id,updated_at DESC,id);
CREATE TABLE onboarding_problem_evidence (
    problem_id uuid NOT NULL REFERENCES onboarding_system_problems(id),
    source_kind text NOT NULL CHECK (source_kind IN ('access','blocked','knowledge')),
    source_id text NOT NULL,
    PRIMARY KEY(problem_id,source_kind,source_id)
);
CREATE TABLE onboarding_problem_history (
    problem_id uuid NOT NULL REFERENCES onboarding_system_problems(id),
    revision bigint NOT NULL,
    actor_id text NOT NULL REFERENCES users(id),
    snapshot jsonb NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(problem_id,revision)
);
CREATE TABLE onboarding_template_publications (
    team_id text NOT NULL REFERENCES teams(id),
    version bigint NOT NULL CHECK (version > 0),
    operation_id uuid NOT NULL UNIQUE,
    author_id text NOT NULL REFERENCES users(id),
    steps jsonb NOT NULL,
    existing_plans boolean NOT NULL,
    published_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(team_id,version)
);

-- Existing plans retain positions, links, status and confirmations. Keys are
-- stable across versions. Omitted keys remain in existing plans.
CREATE FUNCTION apply_published_onboarding_template(target_plan text, template_steps jsonb)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    entry jsonb;
    changed bigint;
    next_position integer;
    any_change boolean := false;
BEGIN
    SELECT COALESCE(max(position),-1)+1 INTO next_position
        FROM onboarding_steps WHERE plan_id=target_plan;
    FOR entry IN SELECT value FROM jsonb_array_elements(template_steps) LOOP
        UPDATE onboarding_steps SET title=entry->>'title',description=entry->>'description'
            WHERE plan_id=target_plan AND COALESCE(template_key,id)=entry->>'key'
            AND (title,description) IS DISTINCT FROM (entry->>'title',entry->>'description');
        GET DIAGNOSTICS changed = ROW_COUNT;
        any_change := any_change OR changed > 0;
        IF NOT EXISTS (SELECT 1 FROM onboarding_steps WHERE plan_id=target_plan AND COALESCE(template_key,id)=entry->>'key') THEN
            INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,template_key)
            VALUES(gen_random_uuid()::text,target_plan,next_position,entry->>'title',entry->>'description','not_started',entry->>'key');
            next_position := next_position+1;
            any_change := true;
        END IF;
    END LOOP;
    IF any_change THEN
        UPDATE onboarding_plans SET revision=revision+1 WHERE id=target_plan;
    END IF;
END;
$$;

CREATE FUNCTION default_published_onboarding_template() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    published jsonb;
BEGIN
    PERFORM id FROM teams WHERE id=NEW.team_id FOR SHARE;
    SELECT steps INTO published FROM onboarding_template_publications
        WHERE team_id=NEW.team_id ORDER BY version DESC LIMIT 1;
    IF published IS NOT NULL THEN
        PERFORM apply_published_onboarding_template(NEW.id,published);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER default_published_onboarding_template AFTER INSERT ON onboarding_plans
    FOR EACH ROW EXECUTE FUNCTION default_published_onboarding_template();
