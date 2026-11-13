package hrscope

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Scope struct {
	Version string `json:"version"`
	Teams   []Team `json:"teams"`
}

type Store struct{ DB *sql.DB }

func (s Store) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/me/access-scope", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, err := s.Current(r.Context())
		if err != nil {
			httpapi.WriteServiceError(w, r, err)
			return
		}
		httpapi.WriteJSON(w, 200, scope)
	}), false))
}

func (s Store) Current(ctx context.Context) (Scope, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return Scope{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `
SELECT a.version::text, t.id, t.name FROM access_scope a
LEFT JOIN teams t ON ($1<>'hr' AND t.id=$2) OR ($1='hr' AND EXISTS (
    SELECT 1 FROM hr_team_assignments h WHERE h.hr_user_id=$3 AND h.team_id=t.id AND h.active
)) ORDER BY t.name,t.id`, p.Role, p.TeamID, p.UserID)
	if err != nil {
		return Scope{}, ports.ErrUnavailable
	}
	defer rows.Close()
	out := Scope{Teams: []Team{}}
	for rows.Next() {
		var id, name sql.NullString
		if err := rows.Scan(&out.Version, &id, &name); err != nil {
			return Scope{}, ports.ErrUnavailable
		}
		if id.Valid {
			out.Teams = append(out.Teams, Team{ID: id.String, Name: name.String})
		}
	}
	if rows.Err() != nil || out.Version == "" {
		return Scope{}, ports.ErrUnavailable
	}
	return out, nil
}

// LockTeam orders queue mutations and assignment revocations for the same team.
func LockTeam(ctx context.Context, tx *sql.Tx, teamID string) error {
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(628113,hashtext($1))", teamID)
	return err
}

func CheckTeam(ctx context.Context, tx *sql.Tx, hrID, teamID string) error {
	var found int
	return tx.QueryRowContext(ctx, `SELECT 1 FROM hr_team_assignments h JOIN users u
ON u.id=h.hr_user_id AND u.role='hr' WHERE h.hr_user_id=$1 AND h.team_id=$2 AND h.active FOR SHARE OF h,u`, hrID, teamID).Scan(&found)
}

func (s Store) Set(ctx context.Context, hrID, teamID string, active bool) error {
	return database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := LockTeam(ctx, tx, teamID); err != nil {
			return err
		}
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM users u CROSS JOIN teams t
WHERE u.id=$1 AND u.role='hr' AND t.id=$2 FOR SHARE OF u,t`, hrID, teamID).Scan(&found); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ports.ErrNotFound
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO hr_team_assignments (hr_user_id,team_id,active)
VALUES ($1,$2,$3) ON CONFLICT (hr_user_id,team_id) DO UPDATE SET active=EXCLUDED.active
WHERE hr_team_assignments.active IS DISTINCT FROM EXCLUDED.active`, hrID, teamID, active); err != nil {
			return err
		}
		if active {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_gap_history(question_id,revision,snapshot)
SELECT id,revision,jsonb_build_object('event','assignment_revoked','specialist_id',hr_id,'direction',direction,'status',status)
FROM knowledge_gaps WHERE direction='hr' AND team_id=$1 AND hr_id=$2 AND status<>'resolved'`, teamID, hrID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE knowledge_gaps SET hr_id=NULL,status='new',
revision=revision+1,updated_at=now() WHERE direction='hr' AND team_id=$1 AND hr_id=$2 AND status<>'resolved'`, teamID, hrID)
		return err
	})
}

func (s Store) List(ctx context.Context) ([][2]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT h.hr_user_id,h.team_id FROM hr_team_assignments h JOIN users u
ON u.id=h.hr_user_id AND u.role='hr' WHERE h.active ORDER BY h.hr_user_id,h.team_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := [][2]string{}
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			return nil, err
		}
		result = append(result, pair)
	}
	return result, rows.Err()
}
