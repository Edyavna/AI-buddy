package integrations

import (
	"context"
	"database/sql"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// Control is a service-side demo operator. It is never injected into Agent or HTTP.
type Control struct{ db *sql.DB }

func NewControl(db *sql.DB, demoMode bool) (*Control, error) {
	if !demoMode {
		return nil, ports.ErrForbidden
	}
	return &Control{db: db}, nil
}

type Record struct {
	RequestID  string `json:"request_id"`
	ExternalID string `json:"external_id"`
	UserID     string `json:"user_id"`
	TeamID     string `json:"team_id"`
	ResourceID string `json:"resource_id"`
	Status     string `json:"status"`
}

func (c *Control) List(ctx context.Context) ([]Record, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT client_reference,external_id,user_id,team_id,resource_id,status
 FROM demo_external.access_requests ORDER BY created_at,client_reference`)
	if err != nil {
		return nil, readError(ctx, err)
	}
	defer rows.Close()
	records := []Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.RequestID, &r.ExternalID, &r.UserID, &r.TeamID, &r.ResourceID, &r.Status); err != nil {
			return nil, readError(ctx, err)
		}
		records = append(records, r)
	}
	return records, readError(ctx, rows.Err())
}

func (c *Control) Count(ctx context.Context) (int, error) {
	var count int
	err := c.db.QueryRowContext(ctx, `SELECT count(*) FROM demo_external.access_requests`).Scan(&count)
	return count, readError(ctx, err)
}

func (c *Control) SetFault(ctx context.Context, id, mode string) error {
	if strings.TrimSpace(id) == "" {
		return ports.ErrInvalid
	}
	switch mode {
	case "none":
		_, err := c.db.ExecContext(ctx, `DELETE FROM demo_external.faults WHERE client_reference=$1`, id)
		return readError(ctx, err)
	case "reject-before-write", "timeout-after-commit", "lookup-unavailable":
		_, err := c.db.ExecContext(ctx, `INSERT INTO demo_external.faults(client_reference,mode) VALUES($1,$2)
ON CONFLICT(client_reference) DO UPDATE SET mode=excluded.mode`, id, mode)
		return readError(ctx, err)
	default:
		return ports.ErrInvalid
	}
}

func (c *Control) Decide(ctx context.Context, id, status, reason string) error {
	if status != "granted" && status != "rejected" {
		return ports.ErrInvalid
	}
	if status == "rejected" && strings.TrimSpace(reason) == "" {
		return ports.ErrInvalid
	}
	if status == "granted" {
		reason = ""
	}
	err := database.WithinTx(ctx, c.db, func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM demo_external.access_requests WHERE client_reference=$1 FOR UPDATE`, id).Scan(&current); err != nil {
			return err
		}
		if current == status {
			return nil
		}
		if current != "pending" {
			return ports.ErrConflict
		}
		if status == "granted" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO demo_external.grants(user_id,team_id,resource_id,permission)
SELECT user_id,team_id,resource_id,permission FROM demo_external.access_requests WHERE client_reference=$1
ON CONFLICT DO NOTHING`, id); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE demo_external.access_requests SET status=$2,failure_reason=$3 WHERE client_reference=$1`, id, status, reason)
		return err
	})
	if err == ports.ErrConflict {
		return err
	}
	return readError(ctx, err)
}

// Seed preserves all decisions and grants on repeated runs.
func Seed(ctx context.Context, db *sql.DB, demoMode bool) error {
	if !demoMode {
		return ports.ErrForbidden
	}
	return database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO demo_external.resources(id,team_id,name,permission,external_id,external_role) VALUES
('repo_demo_backend','team_demo_backend','Тестовый репозиторий','read','synthetic/backend','repository-reader'),
('repo_demo_frontend','team_demo_frontend','Тестовый frontend-репозиторий','read','synthetic/frontend','repository-reader')
ON CONFLICT DO NOTHING;
INSERT INTO demo_external.tasks(id,user_id,team_id,resource_id,title,description) VALUES
('task_demo_1','intern_demo_1','team_demo_backend','repo_demo_backend','Запустить учебный проект','Синтетическая задача. Получить доступ и выполнить инструкцию doc_local_setup.'),
('task_demo_2','intern_demo_2','team_demo_backend','repo_demo_backend','Запустить учебный проект','Синтетическая задача для второго стажёра.'),
('task_demo_other','intern_demo_other','team_demo_frontend','repo_demo_frontend','Запустить учебный frontend','Синтетическая задача другой команды.')
ON CONFLICT DO NOTHING;`)
		return err
	})
}
