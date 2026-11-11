package accessrequests

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Worker struct {
	Store    Store
	Provider ports.AccessProvisioner
}

type job struct {
	id      string
	attempt int64
	owner   identity.Principal
}

// Run has process lifetime, independent of the HTTP connection that confirmed
// a request. The durable due time also serves as a short claim lease.
func (w Worker) Run(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worked, err := w.RunOne(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Warn("access request reconciliation will retry")
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

// RunOne claims a due job and performs one reconciliation without holding a
// database transaction during provider calls.
func (w Worker) RunOne(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var j job
	err := w.Store.DB.QueryRowContext(ctx, `WITH due AS (
 SELECT request_id FROM access_request_jobs WHERE next_attempt_at<=now()
 ORDER BY next_attempt_at,request_id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE access_request_jobs j SET next_attempt_at=now()+interval '5 seconds',attempt=j.attempt+1
FROM due,access_requests r WHERE j.request_id=due.request_id AND r.id=j.request_id
RETURNING j.request_id,j.attempt,r.user_id,r.team_id`).Scan(&j.id, &j.attempt, &j.owner.UserID, &j.owner.TeamID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, storeError(err)
	}
	j.owner.Role = identity.Intern
	ctx = identity.WithPrincipal(ctx, j.owner)
	request, err := w.Store.Get(ctx, j.id)
	if err != nil {
		return true, err
	}
	result, err := w.Provider.Lookup(ctx, j.id)
	if errors.Is(err, ports.ErrNotFound) && request.Status == "submitting" {
		// A missing lookup after a lost reply can race a late external commit.
		// Submit uses the same provider idempotency key even in that case.
		result, err = w.Provider.Submit(ctx, ports.AccessSubmission{
			RequestID: j.id, Resource: request.Resource, Reason: request.Reason,
		})
	}
	if err != nil {
		return true, err
	}
	return true, w.finish(ctx, j, request, result)
}

func (w Worker) finish(ctx context.Context, j job, before ports.RequestState, result ports.AccessResult) error {
	var externalID *string
	var reason string
	switch result.Status {
	case "pending", "granted", "rejected":
		if result.ExternalID == "" || (before.ExternalID != nil && result.ExternalID != *before.ExternalID) {
			return ports.ErrInvalid
		}
		externalID = &result.ExternalID
		if result.Status == "rejected" {
			reason = "Сервис доступа отклонил заявку"
		}
	case "failed":
		if before.Status != "submitting" || result.ExternalID != "" || before.ExternalID != nil {
			return ports.ErrInvalid
		}
		reason = "Сервис доступа не создал заявку. Повторите подтверждение."
	default:
		return ports.ErrInvalid
	}
	return storeError(database.WithinTx(ctx, w.Store.DB, func(tx *sql.Tx) error {
		if err := lockOwner(ctx, tx, j.owner, before.StepID); err != nil {
			return err
		}
		var attempt int64
		err := tx.QueryRowContext(ctx, `SELECT attempt FROM access_request_jobs WHERE request_id=$1 FOR UPDATE`, j.id).Scan(&attempt)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && attempt != j.attempt) {
			return nil
		}
		if err != nil {
			return err
		}
		request, err := scan(tx.QueryRowContext(ctx, `UPDATE access_requests
SET status=$2,external_id=$3,failure_reason=$4,status_checked_at=now(),updated_at=now(),revision=revision+1
WHERE id=$1 AND revision=$5 RETURNING `+columns, j.id, result.Status, externalID, reason, before.Revision))
		if err != nil {
			return err
		}
		if request.Status != before.Status {
			if err := applyPlan(ctx, tx, j.owner, request); err != nil {
				return err
			}
		}
		if request.Status == "pending" {
			_, err = tx.ExecContext(ctx, `UPDATE access_request_jobs
SET next_attempt_at=now()+interval '4 seconds' WHERE request_id=$1`, j.id)
		} else {
			_, err = tx.ExecContext(ctx, `DELETE FROM access_request_jobs WHERE request_id=$1`, j.id)
		}
		return err
	}))
}
