package onboarding

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/jackc/pgx/v5/pgtype"
)

type SQLStore struct{ DB *sql.DB }

func sqlError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	}
	if errors.Is(err, ErrPlanChanged) || errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrInvalid) || errors.Is(err, ports.ErrForbidden) || errors.Is(err, identity.ErrUnauthenticated) {
		return err
	}
	return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
}
func lockPlan(ctx context.Context, tx *sql.Tx, p identity.Principal, write bool) (Snapshot, error) {
	var s Snapshot
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	err := tx.QueryRowContext(ctx, `SELECT id,title,revision FROM onboarding_plans WHERE user_id=$1 AND team_id=$2`+lock, p.UserID, p.TeamID).Scan(&s.ID, &s.Title, &s.Revision)
	return s, err
}
func readSteps(ctx context.Context, tx *sql.Tx, s *Snapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.title,s.description,s.status,s.blocked_reason,s.resource_id,s.permission,
 ARRAY(SELECT r.request_id FROM onboarding_step_requests r WHERE r.step_id=s.id ORDER BY r.request_id),
 ARRAY(SELECT d.prerequisite_id FROM onboarding_step_dependencies d WHERE d.step_id=s.id ORDER BY d.prerequisite_id)
 FROM onboarding_steps s WHERE s.plan_id=$1 ORDER BY s.position`, s.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Steps = make([]Record, 0)
	for rows.Next() {
		var r Record
		var resource, permission sql.NullString
		// pgx database/sql exposes text arrays as strings; pgtype's codec provides
		// the PostgreSQL array parser (including quotes/escapes) without hand parsing.
		var requests, prerequisites string
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &r.Status, &r.BlockedReason, &resource, &permission, &requests, &prerequisites); err != nil {
			return err
		}
		if resource.Valid {
			r.Requirement = &ports.AccessCheck{ResourceID: resource.String, Permission: permission.String}
		}
		codec := pgtype.NewMap()
		if err := codec.Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, []byte(requests), &r.RequestIDs); err != nil {
			return err
		}
		if err := codec.Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, []byte(prerequisites), &r.Prerequisites); err != nil {
			return err
		}
		s.Steps = append(s.Steps, r)
	}
	return rows.Err()
}
func (s SQLStore) Read(ctx context.Context, p identity.Principal) (Snapshot, error) {
	var snapshot Snapshot
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		snapshot, err = lockPlan(ctx, tx, p, false)
		if err != nil {
			return err
		}
		return readSteps(ctx, tx, &snapshot)
	})
	return snapshot, sqlError(err)
}
func (s SQLStore) Patch(ctx context.Context, p identity.Principal, revision int64, id, status string) (Snapshot, error) {
	var snapshot Snapshot
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		snapshot, err = lockPlan(ctx, tx, p, true)
		if err != nil {
			return err
		}
		if snapshot.Revision != revision {
			return ErrPlanChanged
		}
		result, err := tx.ExecContext(ctx, `UPDATE onboarding_steps SET status=$1,blocked_reason=NULL WHERE id=$2 AND plan_id=$3 AND status<>$1`, status, id, snapshot.ID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE onboarding_plans SET revision=revision+1 WHERE id=$1`, snapshot.ID); err != nil {
				return err
			}
			snapshot.Revision++
		}
		return readSteps(ctx, tx, &snapshot)
	})
	return snapshot, sqlError(err)
}

// LockPlanTx establishes the common lock order for #14: plan, then request/job.
// The request-service must use this before locking request rows, and reuse tx.
func LockPlanTx(ctx context.Context, tx *sql.Tx, owner identity.Principal) error {
	p, err := principal(identity.WithPrincipal(ctx, owner))
	if err != nil {
		return err
	}
	_, err = lockPlan(ctx, tx, p, true)
	return sqlError(err)
}

// ApplyRequestStateTx is the plan-side interface for #14. The caller provides
// its exact request transaction and verified owner; never call with model IDs.
// Lock the plan before request rows in all request-service operations. Committing
// the request, job and this update together makes a stale PATCH retry its checks.
func ApplyRequestStateTx(ctx context.Context, tx *sql.Tx, owner identity.Principal, stepID string, request ports.RequestState) error {
	p, err := principal(identity.WithPrincipal(ctx, owner))
	if err != nil {
		return err
	}
	state := request.Status
	switch state {
	case "draft", "submitting", "pending", "granted", "rejected", "failed":
	default:
		return ports.ErrInvalid
	}
	if request.RequestID == "" || request.Resource.ID == "" || request.Resource.Permission == "" || (request.StepID != nil && *request.StepID != stepID) {
		return ports.ErrInvalid
	}
	snapshot, err := lockPlan(ctx, tx, p, true)
	if err != nil {
		return sqlError(err)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM onboarding_steps WHERE id=$1 AND plan_id=$2)`, stepID, snapshot.ID).Scan(&exists); err != nil {
		return sqlError(err)
	}
	if !exists {
		return ports.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO onboarding_step_requests(step_id,request_id,status,resource_id,permission) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(step_id,request_id) DO UPDATE SET status=excluded.status,resource_id=excluded.resource_id,permission=excluded.permission`, stepID, request.RequestID, state, request.Resource.ID, request.Resource.Permission); err != nil {
		return sqlError(err)
	}
	var blocking bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM onboarding_step_requests r WHERE r.step_id=$1 AND
 (r.status IN ('submitting','pending') OR (r.status IN ('failed','rejected') AND NOT EXISTS
 (SELECT 1 FROM onboarding_step_requests g WHERE g.step_id=r.step_id AND g.resource_id=r.resource_id AND g.permission=r.permission AND g.status='granted'))))`, stepID).Scan(&blocking); err != nil {
		return sqlError(err)
	}
	if blocking {
		reason := "Ожидается получение доступа; подробности — в заявке и чате"
		if _, err := tx.ExecContext(ctx, `UPDATE onboarding_steps SET status='blocked',blocked_reason=$2 WHERE id=$1`, stepID, reason); err != nil {
			return sqlError(err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE onboarding_steps SET status='in_progress',blocked_reason=NULL WHERE id=$1 AND status='blocked'
 AND EXISTS(SELECT 1 FROM onboarding_step_requests WHERE step_id=$1 AND status='granted')`, stepID); err != nil {
			return sqlError(err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE onboarding_plans SET revision=revision+1 WHERE id=$1`, snapshot.ID)
	return sqlError(err)
}
