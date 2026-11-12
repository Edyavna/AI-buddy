package mentor

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var ErrAlreadyAssigned = errors.New("intern already assigned")

type AvailableIntern struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Team auth.Team `json:"team"`
}

type MentorAssignment struct {
	InternID string `json:"intern_id"`
	MentorID string `json:"mentor_id"`
	TeamID   string `json:"team_id"`
}

func (s Store) Available(ctx context.Context) ([]AvailableIntern, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id,u.name,t.id,t.name
 FROM users u JOIN teams t ON t.id=u.team_id
 JOIN users m ON m.id=$1 AND m.team_id=u.team_id AND m.role='mentor'
 WHERE u.team_id=$2 AND u.role='intern'
 AND NOT EXISTS(SELECT 1 FROM mentor_assignments a WHERE a.intern_id=u.id)
 ORDER BY u.name,u.id`, p.UserID, p.TeamID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := []AvailableIntern{}
	for rows.Next() {
		var item AvailableIntern
		if err = rows.Scan(&item.ID, &item.Name, &item.Team.ID, &item.Team.Name); err != nil {
			return nil, storeError(err)
		}
		result = append(result, item)
	}
	return result, storeError(rows.Err())
}

func (s Store) Assign(ctx context.Context, internID string) (MentorAssignment, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return MentorAssignment{}, err
	}
	if internID == "" {
		return MentorAssignment{}, ports.ErrNotFound
	}
	result := MentorAssignment{InternID: internID, MentorID: p.UserID, TeamID: p.TeamID}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		// Stabilize both users' team/role through commit. SHARE locks are compatible
		// with the foreign key checks of concurrent INSERTs; uniqueness arbitrates.
		var found string
		if err := tx.QueryRowContext(ctx, `SELECT u.id FROM users u
   JOIN users m ON m.id=$1 AND m.team_id=u.team_id AND m.role='mentor'
   WHERE u.id=$2 AND u.team_id=$3 AND u.role='intern' FOR SHARE OF u,m`, p.UserID, internID, p.TeamID).Scan(&found); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mentor_assignments(intern_id,mentor_id,team_id) VALUES($1,$2,$3)
   ON CONFLICT(intern_id) DO NOTHING`, internID, p.UserID, p.TeamID); err != nil {
			return err
		}
		// A new statement sees the winner after a conflicting INSERT has committed.
		// Never replace another mentor; an identical retry is a read-only success.
		var owner string
		if err := tx.QueryRowContext(ctx, `SELECT mentor_id FROM mentor_assignments WHERE intern_id=$1 AND team_id=$2 FOR SHARE`, internID, p.TeamID).Scan(&owner); err != nil {
			return err
		}
		if owner != p.UserID {
			return ErrAlreadyAssigned
		}
		return nil
	})
	if err != nil {
		return MentorAssignment{}, storeError(err)
	}
	return result, nil
}

// AssignedInternTx authorizes the authenticated mentor in the caller's transaction.
// It retains SHARE locks on assignment and user rows until that transaction ends.
// For checkpoint writes acquire these locks before the plan/step locks.
// This method never uses s.DB, opens a transaction, or commits the caller's tx.
func (s Store) AssignedInternTx(ctx context.Context, tx *sql.Tx, internID string) (identity.Principal, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	if internID == "" {
		return identity.Principal{}, ports.ErrNotFound
	}
	if tx == nil {
		return identity.Principal{}, ports.ErrUnavailable
	}
	items, err := assigned(ctx, tx, p, internID)
	if err != nil {
		return identity.Principal{}, storeError(err)
	}
	return items[0].owner, nil
}
