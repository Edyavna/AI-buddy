CREATE TABLE mentor_assignments (
    intern_id text PRIMARY KEY,
    mentor_id text NOT NULL,
    team_id text NOT NULL,
    FOREIGN KEY (intern_id, team_id) REFERENCES users(id, team_id) ON DELETE CASCADE,
    FOREIGN KEY (mentor_id, team_id) REFERENCES users(id, team_id) ON DELETE CASCADE,
    CHECK (intern_id <> mentor_id)
);

CREATE INDEX mentor_assignments_mentor ON mentor_assignments(mentor_id, team_id, intern_id);
