package onboarding

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	if errors.Is(err, ErrPlanChanged) || errors.Is(err, ErrTransition) || errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrInvalid) || errors.Is(err, ports.ErrForbidden) || errors.Is(err, identity.ErrUnauthenticated) {
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

func checkInternTx(ctx context.Context, tx *sql.Tx, p identity.Principal) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND team_id=$2 AND role='intern' FOR SHARE`,
		p.UserID, p.TeamID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrForbidden
	}
	return err
}
func readSteps(ctx context.Context, tx *sql.Tx, s *Snapshot) error {
	var teamID string
	if err := tx.QueryRowContext(ctx, `SELECT team_id FROM onboarding_plans WHERE id=$1`, s.ID).Scan(&teamID); err == nil && teamID != "" {
		_ = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM hr_team_assignments h JOIN users u ON u.id=h.hr_user_id AND u.role='hr' WHERE h.team_id=$1 AND h.active)`, teamID).Scan(&s.HasHR)
	} else {
		s.HasHR = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.title,s.description,s.status,s.blocked_reason,s.resource_id,s.permission,
 COALESCE(s.template_key,s.id),s.period,s.completion_policy,COALESCE(s.instructions,s.description),s.resources,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('role',c.role,'user_id',c.user_id,'confirmed_at',c.confirmed_at)
 ORDER BY c.role) FROM onboarding_step_confirmations c WHERE c.step_id=s.id),'[]'::jsonb),
 ARRAY(SELECT r.request_id FROM onboarding_step_requests r WHERE r.step_id=s.id ORDER BY r.request_id),
 ARRAY(SELECT d.prerequisite_id FROM onboarding_step_dependencies d WHERE d.step_id=s.id ORDER BY d.prerequisite_id),
 s.blocked_at
 FROM onboarding_steps s WHERE s.plan_id=$1 ORDER BY s.position`, s.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Steps = make([]Record, 0)
	for rows.Next() {
		var r Record
		var resource, permission sql.NullString
		var blockedAt sql.NullTime
		var confirmations, resources []byte
		// pgx database/sql exposes text arrays as strings; pgtype's codec provides
		// the PostgreSQL array parser (including quotes/escapes) without hand parsing.
		var requests, prerequisites string
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &r.Status, &r.BlockedReason, &resource, &permission,
			&r.TemplateKey, &r.Period, &r.CompletionPolicy, &r.Instructions, &resources, &confirmations, &requests, &prerequisites, &blockedAt); err != nil {
			return err
		}
		if blockedAt.Valid {
			t := blockedAt.Time.UTC()
			r.BlockedAt = &t
		}
		if err := json.Unmarshal(resources, &r.Resources); err != nil {
			return err
		}
		if err := json.Unmarshal(confirmations, &r.Confirmations); err != nil {
			return err
		}
		for i := range r.Confirmations {
			r.Confirmations[i].ConfirmedAt = r.Confirmations[i].ConfirmedAt.UTC()
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

// ReadSnapshotTx reads an authorized owner's snapshot. The caller is responsible
// for authorizing the owner before acquiring the plan lock.
func ReadSnapshotTx(ctx context.Context, tx *sql.Tx, owner identity.Principal) (Snapshot, error) {
	snapshot, err := lockPlan(ctx, tx, owner, false)
	if err != nil {
		return Snapshot{}, sqlError(err)
	}
	err = readSteps(ctx, tx, &snapshot)
	return snapshot, sqlError(err)
}

func (s SQLStore) Read(ctx context.Context, p identity.Principal) (Snapshot, error) {
	var snapshot Snapshot
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		snapshot, err = ReadSnapshotTx(ctx, tx, p)
		return err
	})
	return snapshot, sqlError(err)
}

// ReadOnlyPlanTx renders the assigned intern's snapshot with the same progress
// rules. The caller must authorize owner in this transaction before reading it.
func ReadOnlyPlanTx(ctx context.Context, tx *sql.Tx, owner identity.Principal) (Plan, error) {
	snapshot, err := ReadSnapshotTx(ctx, tx, owner)
	if err != nil {
		return Plan{}, sqlError(err)
	}
	return project(snapshot, nil, identity.Principal{}), nil
}

func (s SQLStore) Patch(ctx context.Context, p identity.Principal, revision int64, id, status string) (Snapshot, error) {
	var snapshot Snapshot
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := checkInternTx(ctx, tx, p); err != nil {
			return err
		}
		var err error
		snapshot, err = lockPlan(ctx, tx, p, true)
		if err != nil {
			return err
		}
		if snapshot.Revision != revision {
			return ErrPlanChanged
		}
		if err := readSteps(ctx, tx, &snapshot); err != nil {
			return err
		}
		step, err := findStep(snapshot, id)
		if err != nil || step.Status == status {
			return err
		}
		if err := saveStepStatus(ctx, tx, id, status); err != nil {
			return err
		}
		if step.CompletionPolicy == "intern" && (status == "completed" || step.Status == "completed") {
			if err := writeConfirmation(ctx, tx, id, p, status == "completed"); err != nil {
				return err
			}
		}
		if err := advanceRevision(ctx, tx, &snapshot); err != nil {
			return err
		}
		return readSteps(ctx, tx, &snapshot)
	})
	return snapshot, sqlError(err)
}

func findStep(snapshot Snapshot, id string) (Record, error) {
	for _, step := range snapshot.Steps {
		if step.ID == id {
			return step, nil
		}
	}
	return Record{}, ports.ErrNotFound
}

func saveStepStatus(ctx context.Context, tx *sql.Tx, id, status string) error {
	var err error
	if status == "blocked" {
		_, err = tx.ExecContext(ctx, `UPDATE onboarding_steps SET status=$1 WHERE id=$2`, status, id)
		if err == nil {
			err = recordBlockingTx(ctx, tx, id, "Заблокирован")
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE onboarding_steps SET status=$1,blocked_reason=NULL WHERE id=$2`, status, id)
		if err == nil {
			err = recordUnblockingTx(ctx, tx, id)
		}
	}
	return err
}

func recordBlockingTx(ctx context.Context, tx *sql.Tx, stepID, reason string) error {
	now := time.Now().UTC()
	var prevBlockedAt sql.NullTime
	_ = tx.QueryRowContext(ctx, `SELECT blocked_at FROM onboarding_steps WHERE id=$1`, stepID).Scan(&prevBlockedAt)
	if !prevBlockedAt.Valid {
		eventID := fmt.Sprintf("%s:%d", stepID, now.UnixNano())
		if _, err := tx.ExecContext(ctx, `INSERT INTO onboarding_step_blocking_history(id, step_id, blocked_reason, started_at) VALUES($1, $2, $3, $4)`, eventID, stepID, reason, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE onboarding_steps SET blocked_at=$1 WHERE id=$2`, now, stepID); err != nil {
			return err
		}
	}
	return nil
}

func recordUnblockingTx(ctx context.Context, tx *sql.Tx, stepID string) error {
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE onboarding_step_blocking_history SET ended_at=$1 WHERE step_id=$2 AND ended_at IS NULL`, now, stepID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE onboarding_steps SET blocked_at=NULL WHERE id=$1`, stepID); err != nil {
		return err
	}
	return nil
}

func advanceRevision(ctx context.Context, tx *sql.Tx, snapshot *Snapshot) error {
	return tx.QueryRowContext(ctx, `UPDATE onboarding_plans SET revision=revision+1 WHERE id=$1 RETURNING revision`, snapshot.ID).Scan(&snapshot.Revision)
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
	result, err := tx.ExecContext(ctx, `INSERT INTO onboarding_step_requests AS r(step_id,request_id,status,resource_id,permission) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(step_id,request_id) DO UPDATE SET status=excluded.status,resource_id=excluded.resource_id,permission=excluded.permission
 WHERE (r.status,r.resource_id,r.permission) IS DISTINCT FROM (excluded.status,excluded.resource_id,excluded.permission)`, stepID, request.RequestID, state, request.Resource.ID, request.Resource.Permission)
	if err != nil {
		return sqlError(err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
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
		if err := recordBlockingTx(ctx, tx, stepID, reason); err != nil {
			return sqlError(err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE onboarding_steps s SET status=CASE
 WHEN (SELECT count(*) FROM onboarding_step_confirmations c WHERE c.step_id=s.id
       AND (s.completion_policy='both' OR c.role=s.completion_policy))
      = CASE s.completion_policy WHEN 'both' THEN 2 ELSE 1 END THEN 'completed'
 ELSE 'in_progress' END,blocked_reason=NULL WHERE id=$1 AND status='blocked'
 AND EXISTS(SELECT 1 FROM onboarding_step_requests WHERE step_id=$1 AND status='granted')`, stepID); err != nil {
			return sqlError(err)
		}
		if err := recordUnblockingTx(ctx, tx, stepID); err != nil {
			return sqlError(err)
		}
	}
	return sqlError(advanceRevision(ctx, tx, &snapshot))
}
