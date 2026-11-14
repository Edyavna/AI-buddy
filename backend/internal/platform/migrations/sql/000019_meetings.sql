CREATE TABLE onboarding_meetings (
    id uuid PRIMARY KEY,
    intern_id text NOT NULL REFERENCES users(id),
    mentor_id text NOT NULL REFERENCES users(id),
    team_id text NOT NULL REFERENCES teams(id),
    previous_id uuid REFERENCES onboarding_meetings(id),
    scheduled_at timestamptz NOT NULL,
    timezone text NOT NULL,
    status text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','completed','cancelled')),
    revision bigint NOT NULL DEFAULT 1,
    result text NOT NULL DEFAULT '',
    create_hash text NOT NULL,
    finish_hash text,
    completed_at timestamptz,
    created_by text NOT NULL REFERENCES users(id)
);
CREATE INDEX onboarding_meetings_participants ON onboarding_meetings(intern_id,mentor_id,team_id,scheduled_at DESC);
CREATE TABLE onboarding_meeting_topics (
    id uuid PRIMARY KEY,
    meeting_id uuid NOT NULL REFERENCES onboarding_meetings(id),
    author_id text NOT NULL REFERENCES users(id),
    content text NOT NULL,
    private boolean NOT NULL DEFAULT false
);
CREATE TABLE onboarding_agreements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id uuid NOT NULL REFERENCES onboarding_meetings(id),
    owner_id text NOT NULL REFERENCES users(id),
    content text NOT NULL,
    due_at timestamptz,
    step_id text REFERENCES onboarding_steps(id),
    completed_at timestamptz,
    revision bigint NOT NULL DEFAULT 1
);
CREATE TABLE onboarding_meeting_history (
    meeting_id uuid NOT NULL REFERENCES onboarding_meetings(id),
    revision bigint NOT NULL,
    actor_id text NOT NULL REFERENCES users(id),
    action text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(meeting_id,revision)
);
CREATE TABLE onboarding_agreement_history (
    agreement_id uuid NOT NULL REFERENCES onboarding_agreements(id),
    revision bigint NOT NULL,
    actor_id text NOT NULL REFERENCES users(id),
    action text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(agreement_id,revision)
);
-- Record future checkpoint changes without guessing dates for legacy checkpoints.
ALTER TABLE onboarding_steps ADD COLUMN meeting_changed_at timestamptz;
CREATE FUNCTION record_meeting_checkpoint_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.meeting_changed_at := clock_timestamp();
    RETURN NEW;
END;
$$;
CREATE TRIGGER record_meeting_checkpoint_change BEFORE INSERT OR UPDATE OF status,title,description ON onboarding_steps
    FOR EACH ROW EXECUTE FUNCTION record_meeting_checkpoint_change();
