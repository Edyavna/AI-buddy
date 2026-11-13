-- #59 Подготовка к ревью PR: сохраняемый черновик/отправка, чек-лист команды,
-- результаты проверок с источником и история ревизий.

-- Шаблон чек-листа на команду. Пока строки нет, действует встроенный шаблон по умолчанию.
CREATE TABLE review_checklist_templates (
    team_id text PRIMARY KEY REFERENCES teams(id),
    items jsonb NOT NULL CHECK (jsonb_typeof(items) = 'array'),
    updated_by text NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (updated_by, team_id) REFERENCES users(id, team_id)
);

CREATE TABLE review_preparations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id text NOT NULL,
    intern_id text NOT NULL,
    mentor_id text NOT NULL,
    step_id text REFERENCES onboarding_steps(id),
    pr_url text NOT NULL,
    pr_repo text NOT NULL,
    pr_number integer NOT NULL CHECK (pr_number > 0),
    pr_title text NOT NULL DEFAULT '',
    pr_head_sha text NOT NULL DEFAULT '',
    pr_source text NOT NULL DEFAULT 'manual' CHECK (pr_source IN ('github', 'manual')),
    pr_fetch_error text NOT NULL DEFAULT '',
    pr_fetched_at timestamptz,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'submitted')),
    revision bigint NOT NULL DEFAULT 1,
    answers jsonb NOT NULL DEFAULT '{}'::jsonb,
    description text NOT NULL DEFAULT '',
    submitted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (intern_id, team_id) REFERENCES users(id, team_id),
    FOREIGN KEY (mentor_id, team_id) REFERENCES users(id, team_id)
);
CREATE INDEX review_preparations_intern ON review_preparations (intern_id, team_id, updated_at DESC, id);
CREATE INDEX review_preparations_mentor ON review_preparations (mentor_id, team_id, updated_at DESC, id);
-- У стажёра не больше одного черновика; повторное создание возвращает существующий.
CREATE UNIQUE INDEX review_preparations_one_draft ON review_preparations (intern_id, team_id) WHERE status = 'draft';

-- Результаты проверок с обязательным источником: intern — указан стажёром, ci — получен из интеграции.
CREATE TABLE review_preparation_checks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_id uuid NOT NULL REFERENCES review_preparations(id) ON DELETE CASCADE,
    name text NOT NULL,
    status text NOT NULL CHECK (status IN ('passed', 'failed', 'skipped')),
    source text NOT NULL CHECK (source IN ('intern', 'ci')),
    detail text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_preparation_checks_prep ON review_preparation_checks (preparation_id, created_at, id);

-- Снимок предыдущей ревизии фиксируется при каждой отправке на ревью.
CREATE TABLE review_preparation_history (
    preparation_id uuid NOT NULL REFERENCES review_preparations(id) ON DELETE CASCADE,
    revision bigint NOT NULL,
    snapshot jsonb NOT NULL,
    submitted_at timestamptz NOT NULL,
    PRIMARY KEY (preparation_id, revision)
);
