package accessrequests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/drafts"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var ErrDraftChanged = errors.New("draft version changed")

type Store struct{ DB *sql.DB }

var _ drafts.Store = Store{}
var _ ports.RequestReader = Store{}

const columns = `id,conversation_id,assistant_message_id,step_id,resource_id,resource_name,permission,reason,
draft_version,revision,created_at,updated_at,status,external_id,status_checked_at,failure_reason`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (ports.RequestState, error) {
	var request ports.RequestState
	err := row.Scan(&request.RequestID, &request.ConversationID, &request.AssistantMessageID, &request.StepID,
		&request.Resource.ID, &request.Resource.Name, &request.Resource.Permission, &request.Reason,
		&request.Version, &request.Revision, &request.CreatedAt, &request.UpdatedAt,
		&request.Status, &request.ExternalID, &request.StatusCheckedAt, &request.FailureReason)
	return request, err
}

func intern(ctx context.Context) (identity.Principal, error) {
	owner, err := identity.Require(ctx)
	if err != nil {
		return owner, err
	}
	if owner.Role != identity.Intern {
		return owner, ports.ErrForbidden
	}
	return owner, nil
}

func storeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ports.ErrNotFound
	case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrConflict), errors.Is(err, ErrDraftChanged),
		errors.Is(err, ports.ErrForbidden), errors.Is(err, ports.ErrInvalid), errors.Is(err, identity.ErrUnauthenticated):
		return err
	default:
		return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
	}
}

func (s Store) Get(ctx context.Context, id string) (ports.RequestState, error) {
	owner, err := intern(ctx)
	if err != nil {
		return ports.RequestState{}, err
	}
	request, err := scan(s.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM access_requests
WHERE id=$1 AND user_id=$2 AND team_id=$3`, id, owner.UserID, owner.TeamID))
	return request, storeError(err)
}

func (s Store) FindActive(ctx context.Context, check ports.AccessCheck) (ports.RequestState, error) {
	owner, err := intern(ctx)
	if err != nil {
		return ports.RequestState{}, err
	}
	request, err := scan(s.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM access_requests
WHERE user_id=$1 AND team_id=$2 AND resource_id=$3 AND permission=$4 AND status IN ('draft','submitting','pending')`,
		owner.UserID, owner.TeamID, check.ResourceID, check.Permission))
	return request, storeError(err)
}

func (s Store) List(ctx context.Context) ([]ports.RequestState, error) {
	owner, err := intern(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+columns+` FROM access_requests
WHERE user_id=$1 AND team_id=$2 ORDER BY created_at DESC,id`, owner.UserID, owner.TeamID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	requests := []ports.RequestState{}
	for rows.Next() {
		request, err := scan(rows)
		if err != nil {
			return nil, storeError(err)
		}
		requests = append(requests, request)
	}
	return requests, storeError(rows.Err())
}

// All writers take the owner lock before the plan and request rows. This also
// serializes edits and confirmation for conversations that have no plan step.
func lockOwner(ctx context.Context, tx *sql.Tx, owner identity.Principal, stepID *string) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,14))`, owner.UserID); err != nil {
		return err
	}
	if stepID != nil {
		return onboarding.LockPlanTx(ctx, tx, owner)
	}
	return nil
}

func applyPlan(ctx context.Context, tx *sql.Tx, owner identity.Principal, request ports.RequestState) error {
	if request.StepID == nil {
		return nil
	}
	return onboarding.ApplyRequestStateTx(ctx, tx, owner, *request.StepID, request)
}

type draftTx struct{ tx *sql.Tx }

func (s Store) WithinTx(ctx context.Context, fn func(drafts.Tx) error) error {
	return storeError(database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		return fn(draftTx{tx})
	}))
}

func (d draftTx) UpsertDraft(ctx context.Context, owner identity.Principal, input ports.DraftInput) (ports.Draft, error) {
	if err := lockOwner(ctx, d.tx, owner, input.StepID); err != nil {
		return ports.Draft{}, err
	}
	var exists bool
	if err := d.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversations c
JOIN chat_messages m ON m.conversation_id=c.id
WHERE c.id=$1 AND c.user_id=$2 AND c.team_id=$3 AND m.id=$4 AND c.step_id IS NOT DISTINCT FROM $5)`,
		input.ConversationID, owner.UserID, owner.TeamID, input.AssistantMessageID, input.StepID).Scan(&exists); err != nil {
		return ports.Draft{}, err
	}
	if !exists {
		return ports.Draft{}, ports.ErrNotFound
	}
	if err := d.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM access_requests WHERE user_id=$1 AND team_id=$2
AND resource_id=$3 AND permission=$4 AND id<>$5 AND status IN ('draft','submitting','pending','granted'))`,
		owner.UserID, owner.TeamID, input.Resource.ID, input.Resource.Permission, input.RequestID).Scan(&exists); err != nil {
		return ports.Draft{}, err
	}
	if exists {
		return ports.Draft{}, ports.ErrConflict
	}
	before, err := scan(d.tx.QueryRowContext(ctx, `SELECT `+columns+` FROM access_requests WHERE id=$1 FOR UPDATE`, input.RequestID))
	var request ports.RequestState
	switch {
	case errors.Is(err, sql.ErrNoRows):
		request, err = scan(d.tx.QueryRowContext(ctx, `INSERT INTO access_requests
(id,user_id,team_id,conversation_id,assistant_message_id,step_id,resource_id,resource_name,permission,reason)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING RETURNING `+columns,
			input.RequestID, owner.UserID, owner.TeamID, input.ConversationID, input.AssistantMessageID,
			input.StepID, input.Resource.ID, input.Resource.Name, input.Resource.Permission, input.Reason))
		if errors.Is(err, sql.ErrNoRows) {
			return ports.Draft{}, ports.ErrConflict
		}
	case err != nil:
		return ports.Draft{}, err
	case before.ConversationID != input.ConversationID || before.Status != "draft":
		return ports.Draft{}, ports.ErrConflict
	default:
		changed := before.Resource != input.Resource || before.Reason != input.Reason
		if !changed && before.AssistantMessageID == input.AssistantMessageID {
			return before.Draft, nil
		}
		var versionDelta int64
		if changed {
			versionDelta = 1
		}
		request, err = scan(d.tx.QueryRowContext(ctx, `UPDATE access_requests
SET assistant_message_id=$2,resource_id=$3,resource_name=$4,permission=$5,reason=$6,
draft_version=draft_version+$7,revision=revision+1,updated_at=now() WHERE id=$1 RETURNING `+columns,
			input.RequestID, input.AssistantMessageID, input.Resource.ID, input.Resource.Name,
			input.Resource.Permission, input.Reason, versionDelta))
	}
	if err != nil {
		return ports.Draft{}, err
	}
	if err := applyPlan(ctx, d.tx, owner, request); err != nil {
		return ports.Draft{}, err
	}
	return request.Draft, nil
}

func (d draftTx) LinkAssistant(ctx context.Context, owner identity.Principal, assistantID, requestID string) error {
	return chat.LinkRequestTx(ctx, d.tx, owner, assistantID, requestID)
}

// Confirm accepts only a version checked against the provider. The request is
// reread under locks so a concurrent repeat can return its current snapshot.
func (s Store) Confirm(ctx context.Context, id string, version int64, checker ports.AccessChecker) (ports.RequestState, error) {
	owner, err := intern(ctx)
	if err != nil {
		return ports.RequestState{}, err
	}
	before, err := s.Get(ctx, id)
	if err != nil {
		return ports.RequestState{}, err
	}
	var checkErr error
	if (before.Status == "draft" || before.Status == "failed") && before.Version == version {
		if checker == nil {
			checkErr = ports.ErrUnavailable
		} else {
			granted, err := checker.Check(ctx, ports.AccessCheck{ResourceID: before.Resource.ID, Permission: before.Resource.Permission})
			checkErr = err
			if err == nil && granted {
				checkErr = ports.ErrConflict
			}
		}
	}
	var request ports.RequestState
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := lockOwner(ctx, tx, owner, before.StepID); err != nil {
			return err
		}
		request, err = scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM access_requests
WHERE id=$1 AND user_id=$2 AND team_id=$3 FOR UPDATE`, id, owner.UserID, owner.TeamID))
		if err != nil {
			return err
		}
		if request.Status != "draft" && request.Status != "failed" {
			return nil
		}
		if request.Status == "failed" && (before.Status != "failed" || before.Revision != request.Revision) {
			return nil
		}
		if request.Version != version || before.Version != version {
			return ErrDraftChanged
		}
		if checkErr != nil {
			return checkErr
		}
		var conflict bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM access_requests
WHERE user_id=$1 AND team_id=$2 AND resource_id=$3 AND permission=$4 AND id<>$5
AND status IN ('draft','submitting','pending','granted'))`, owner.UserID, owner.TeamID,
			request.Resource.ID, request.Resource.Permission, id).Scan(&conflict); err != nil {
			return err
		}
		if conflict {
			return ports.ErrConflict
		}
		request, err = scan(tx.QueryRowContext(ctx, `UPDATE access_requests
SET status='submitting',failure_reason='',status_checked_at=NULL,revision=revision+1,updated_at=now()
WHERE id=$1 RETURNING `+columns, id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_request_jobs(request_id) VALUES($1)`, id); err != nil {
			return err
		}
		return applyPlan(ctx, tx, owner, request)
	})
	return request, storeError(err)
}
