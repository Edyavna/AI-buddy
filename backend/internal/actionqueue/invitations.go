package actionqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var errInvitationChanged = errors.New("invitation request changed")

type Store struct {
	DB          *sql.DB
	Checkpoints onboarding.Checkpoints
}

type Invitation struct {
	ID          string     `json:"id"`
	State       string     `json:"state"`
	RequestedAt time.Time  `json:"requested_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Note        *string    `json:"note"`
}

type invitationChange struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

func requireRole(ctx context.Context, roles ...identity.Role) (identity.Principal, error) {
	actor, err := identity.Require(ctx)
	if err != nil {
		return actor, err
	}
	for _, role := range roles {
		if actor.Role == role {
			return actor, nil
		}
	}
	return actor, ports.ErrForbidden
}

func storeError(err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ports.ErrNotFound
	case err == nil, errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrForbidden), errors.Is(err, errInvitationChanged):
		return err
	default:
		return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
	}
}

const invitationColumns = "id::text,state,requested_at,closed_at,note"

func readInvitation(row *sql.Row) (Invitation, error) {
	var result Invitation
	if err := row.Scan(&result.ID, &result.State, &result.RequestedAt, &result.ClosedAt, &result.Note); err != nil {
		return result, err
	}
	result.RequestedAt = result.RequestedAt.UTC()
	if result.ClosedAt != nil {
		closed := result.ClosedAt.UTC()
		result.ClosedAt = &closed
	}
	return result, nil
}

func (s Store) internInvitation(ctx context.Context, step string, change *invitationChange) (*Invitation, error) {
	actor, err := requireRole(ctx, identity.Intern)
	if err != nil {
		return nil, err
	}
	var result *Invitation
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if change != nil && change.Action == "request" {
			var assigned bool
			err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mentor_assignments a
JOIN users m ON m.id=a.mentor_id AND m.team_id=a.team_id AND m.role='mentor'
WHERE a.intern_id=$1 AND a.team_id=$2 FOR SHARE OF a,m)`, actor.UserID, actor.TeamID).Scan(&assigned)
			if err != nil {
				return err
			}
			if !assigned {
				return errInvitationChanged
			}
		}
		var id string
		err := tx.QueryRowContext(ctx, `SELECT s.id FROM onboarding_steps s
JOIN onboarding_plans p ON p.id=s.plan_id JOIN users u ON u.id=p.user_id AND u.team_id=p.team_id AND u.role='intern'
WHERE s.id=$1 AND s.template_key='team-setup' AND p.user_id=$2 AND p.team_id=$3 FOR UPDATE OF p`, step, actor.UserID, actor.TeamID).Scan(&id)
		if err != nil {
			return err
		}
		if change == nil {
			item, err := readInvitation(tx.QueryRowContext(ctx, `SELECT `+invitationColumns+` FROM checkpoint_invitation_requests
WHERE step_id=$1 ORDER BY requested_at DESC,id DESC LIMIT 1`, id))
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			result = &item
			return err
		}
		if change.Action == "request" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO checkpoint_invitation_requests(id,step_id)
VALUES($1,$2) ON CONFLICT DO NOTHING`, change.ID, id); err != nil {
				return err
			}
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE checkpoint_invitation_requests SET state='canceled',closed_at=clock_timestamp()
WHERE id=$1 AND step_id=$2 AND state='pending'`, change.ID, id)
			if err != nil {
				return err
			}
		}
		item, err := readInvitation(tx.QueryRowContext(ctx, "SELECT "+invitationColumns+" FROM checkpoint_invitation_requests WHERE id=$1 AND step_id=$2", change.ID, id))
		if errors.Is(err, sql.ErrNoRows) || (err == nil && change.Action == "cancel" && item.State != "canceled") {
			return errInvitationChanged
		}
		result = &item
		return err
	})
	return result, storeError(err)
}

func (s Store) mentorInvitation(ctx context.Context, intern, id string, note *string) (Invitation, error) {
	actor, err := requireRole(ctx, identity.Mentor)
	if err != nil {
		return Invitation{}, err
	}
	var result Invitation
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		owner, err := s.Checkpoints.Assignments.AssignedInternTx(ctx, tx, intern)
		if err != nil {
			return err
		}
		if err := onboarding.LockPlanTx(ctx, tx, owner); err != nil {
			return err
		}
		scope := `i.id=$1 AND EXISTS(SELECT 1 FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id
WHERE s.id=i.step_id AND p.user_id=$2 AND p.team_id=$3)`
		if note != nil {
			if _, err = tx.ExecContext(ctx, `UPDATE checkpoint_invitation_requests i
SET state='completed',closed_at=clock_timestamp(),completed_by=$4,note=NULLIF($5,'') WHERE `+scope+` AND i.state='pending'`,
				id, owner.UserID, owner.TeamID, actor.UserID, *note); err != nil {
				return err
			}
		}
		result, err = readInvitation(tx.QueryRowContext(ctx, "SELECT "+invitationColumns+" FROM checkpoint_invitation_requests i WHERE "+scope, id, owner.UserID, owner.TeamID))
		if err == nil && note != nil && result.State != "completed" {
			return errInvitationChanged
		}
		return err
	})
	return result, storeError(err)
}
