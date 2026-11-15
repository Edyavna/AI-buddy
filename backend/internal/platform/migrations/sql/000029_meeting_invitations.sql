ALTER TABLE onboarding_meetings
    DROP CONSTRAINT onboarding_meetings_status_check,
    ADD CONSTRAINT onboarding_meetings_status_check
        CHECK (status IN ('scheduled','pending','accepted','declined','completed','cancelled')),
    ADD COLUMN title text NOT NULL DEFAULT '',
    ADD COLUMN call_url text NOT NULL DEFAULT '',
    ADD COLUMN selected_agenda boolean NOT NULL DEFAULT false,
    ADD COLUMN last_decision_revision bigint;

CREATE TABLE onboarding_meeting_agenda_choices (
    meeting_id uuid NOT NULL REFERENCES onboarding_meetings(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('checkpoint','question','agreement')),
    item_id text NOT NULL,
    included boolean NOT NULL,
    PRIMARY KEY (meeting_id,kind,item_id)
);
