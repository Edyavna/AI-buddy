CREATE TABLE checkpoint_invitation_requests (
    id uuid PRIMARY KEY,
    step_id text NOT NULL REFERENCES onboarding_steps(id) ON DELETE CASCADE,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','canceled','completed')),
    closed_at timestamptz,
    completed_by text REFERENCES users(id),
    note text CHECK (char_length(note) <= 500),
    CHECK ((state = 'pending') = (closed_at IS NULL)),
    CHECK ((state = 'completed') = (completed_by IS NOT NULL)),
    CHECK (state = 'completed' OR note IS NULL)
);
CREATE UNIQUE INDEX checkpoint_invitation_pending ON checkpoint_invitation_requests(step_id) WHERE state = 'pending';
CREATE INDEX checkpoint_invitation_history ON checkpoint_invitation_requests(step_id, requested_at DESC, id DESC);

ALTER TABLE onboarding_steps ADD COLUMN status_changed_at timestamptz NOT NULL DEFAULT transaction_timestamp();
CREATE FUNCTION timestamp_checkpoint_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.status_changed_at := clock_timestamp();
    RETURN NEW;
END;
$$;
CREATE TRIGGER checkpoint_status_time BEFORE UPDATE OF status ON onboarding_steps
    FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status) EXECUTE FUNCTION timestamp_checkpoint_status();

ALTER TABLE knowledge_gaps ADD COLUMN waiting_since timestamptz;
UPDATE knowledge_gaps SET waiting_since = created_at;
ALTER TABLE knowledge_gaps ALTER COLUMN waiting_since SET NOT NULL, ALTER COLUMN waiting_since SET DEFAULT clock_timestamp();
CREATE FUNCTION timestamp_question_wait() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.waiting_since := clock_timestamp();
    RETURN NEW;
END;
$$;
CREATE TRIGGER question_wait_time BEFORE UPDATE OF direction, status ON knowledge_gaps
    FOR EACH ROW WHEN (OLD.direction IS DISTINCT FROM NEW.direction OR
                       (OLD.status = 'resolved' AND NEW.status <> 'resolved'))
    EXECUTE FUNCTION timestamp_question_wait();

CREATE TABLE action_queue_completions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('invitation','confirmation','question','hr_claim')),
    direction text NOT NULL CHECK (direction IN ('mentor','hr')),
    appeared_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL
);
CREATE INDEX action_queue_completions_time ON action_queue_completions(completed_at);

CREATE FUNCTION record_invitation_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO action_queue_completions(kind,direction,appeared_at,completed_at)
        VALUES ('invitation','mentor',NEW.requested_at,NEW.closed_at);
    RETURN NULL;
END;
$$;
CREATE TRIGGER invitation_queue_completion AFTER UPDATE OF state ON checkpoint_invitation_requests
    FOR EACH ROW WHEN (OLD.state = 'pending' AND NEW.state = 'completed')
    EXECUTE FUNCTION record_invitation_completion();

CREATE FUNCTION record_confirmation_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO action_queue_completions(kind,direction,appeared_at,completed_at)
        SELECT 'confirmation','mentor',
               LEAST(NEW.confirmed_at, GREATEST(s.status_changed_at,c.confirmed_at,
                   (SELECT max(d.status_changed_at) FROM onboarding_step_dependencies r
                    JOIN onboarding_steps d ON d.id=r.prerequisite_id WHERE r.step_id=s.id))),
               NEW.confirmed_at
        FROM onboarding_steps s
        LEFT JOIN onboarding_step_confirmations c ON c.step_id=s.id AND c.role='intern'
        WHERE s.id=NEW.step_id AND (s.completion_policy='mentor' OR c.step_id IS NOT NULL);
    RETURN NULL;
END;
$$;
CREATE TRIGGER confirmation_queue_completion AFTER INSERT ON onboarding_step_confirmations
    FOR EACH ROW WHEN (NEW.role = 'mentor') EXECUTE FUNCTION record_confirmation_completion();

CREATE FUNCTION record_question_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO action_queue_completions(kind,direction,appeared_at,completed_at)
        VALUES (CASE WHEN NEW.status='resolved' THEN 'question' ELSE 'hr_claim' END,
                NEW.direction, OLD.waiting_since, GREATEST(NEW.updated_at,OLD.waiting_since));
    RETURN NULL;
END;
$$;
CREATE TRIGGER question_queue_completion AFTER UPDATE OF status ON knowledge_gaps
    FOR EACH ROW WHEN (OLD.direction = NEW.direction AND
        ((OLD.status <> 'resolved' AND NEW.status='resolved') OR
         (OLD.direction='hr' AND OLD.status='new' AND NEW.status='in_progress' AND NEW.hr_id IS NOT NULL)))
    EXECUTE FUNCTION record_question_completion();
