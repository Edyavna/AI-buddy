// Package demo contains explicit synthetic fixtures, never production fallbacks.
package demo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Edyavna/AI-buddy/backend/internal/checklist"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// Seed inserts missing fixtures only. Existing progress, sessions and requests
// survive repeated runs; GET/login never creates a plan as a side effect.
func Seed(ctx context.Context, db *sql.DB, enabled bool) error {
	if !enabled {
		return errors.New("demo seed requires DEMO_MODE=true")
	}
	err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		statements := []string{
			`INSERT INTO teams(id,name) VALUES ('team_demo_backend','Тестовая backend-команда'),('team_demo_frontend','Тестовая frontend-команда') ON CONFLICT DO NOTHING`,
			`INSERT INTO users(id,team_id,name,role,demo_account) VALUES
    ('intern_demo_1','team_demo_backend','Тестовый стажер','intern',true),
    ('intern_demo_2','team_demo_backend','Второй стажер','intern',true),
    ('intern_demo_unassigned','team_demo_backend','Свободный стажёр','intern',true),
    ('intern_demo_other','team_demo_frontend','Стажер другой команды','intern',true),
    ('mentor_demo_1','team_demo_backend','Тестовый наставник','mentor',true),
    ('mentor_demo_2','team_demo_backend','Второй наставник','mentor',true) ON CONFLICT DO NOTHING`,
			`INSERT INTO mentor_assignments(intern_id,mentor_id,team_id) VALUES
    ('intern_demo_1','mentor_demo_1','team_demo_backend'),
    ('intern_demo_2','mentor_demo_2','team_demo_backend') ON CONFLICT DO NOTHING`,
			`INSERT INTO onboarding_plans(id,user_id,team_id,title) VALUES
    ('plan_demo_1','intern_demo_1','team_demo_backend','Подготовка к первой задаче'),
    ('plan_demo_2','intern_demo_2','team_demo_backend','Подготовка к первой задаче'),
    ('plan_demo_unassigned','intern_demo_unassigned','team_demo_backend','План адаптации'),
    ('plan_demo_other','intern_demo_other','team_demo_frontend','Подготовка к первой задаче') ON CONFLICT DO NOTHING`,
			`INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,resource_id,permission) VALUES
    ('step_local_setup','plan_demo_1',0,'Запустить проект','Тестовый шаг настройки окружения','in_progress','repo_demo_backend','read'),
    ('step_local_setup_2','plan_demo_2',0,'Запустить проект','Тестовый шаг настройки окружения','in_progress','repo_demo_backend','read'),
    ('step_local_setup_other','plan_demo_other',0,'Запустить проект','Тестовый шаг настройки окружения','in_progress','repo_demo_frontend','read') ON CONFLICT DO NOTHING`,
		}
		for _, q := range statements {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return checklist.SyncDemoPlansTx(ctx, tx)
	})
	if err != nil {
		return err
	}
	return integrations.Seed(ctx, db, true)
}

// Access is a legacy read-only fixture for explicit callers. No grant is issued
// by this stub; a pending/nonexistent request never counts as an access right.
type Access struct{}

func (Access) Check(ctx context.Context, check ports.AccessCheck) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p, err := identity.Require(ctx)
	if err != nil {
		return false, err
	}
	if p.Role != identity.Intern {
		return false, ports.ErrForbidden
	}
	resource := ""
	switch p.TeamID {
	case "team_demo_backend":
		resource = "repo_demo_backend"
	case "team_demo_frontend":
		resource = "repo_demo_frontend"
	}
	if resource == "" || check.ResourceID != resource || check.Permission != "read" {
		return false, ports.ErrNotFound
	}
	return false, nil
}
