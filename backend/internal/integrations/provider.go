package integrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// Provider implements the corporate boundary using persistent synthetic data.
type Provider struct{ DB *sql.DB }

var _ ports.TaskProvider = (*Provider)(nil)
var _ ports.AccessChecker = (*Provider)(nil)
var _ ports.AccessProvider = (*Provider)(nil)

func New(db *sql.DB) *Provider { return &Provider{DB: db} }

func principal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return p, err
	}
	if p.Role != identity.Intern {
		return p, ports.ErrForbidden
	}
	return p, ctx.Err()
}

func readError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	}
	return ports.ErrUnavailable
}

func (p *Provider) FirstTask(ctx context.Context) (ports.Task, error) {
	owner, err := principal(ctx)
	if err != nil {
		return ports.Task{}, err
	}
	var task ports.Task
	err = p.DB.QueryRowContext(ctx, `
 SELECT t.id,t.title,t.description,t.url,r.id,r.name,r.permission
 FROM demo_external.tasks t
 JOIN demo_external.resources r ON r.id=t.resource_id AND r.team_id=t.team_id
 WHERE t.user_id=$1 AND t.team_id=$2 ORDER BY t.position,t.id LIMIT 1`,
		owner.UserID, owner.TeamID).Scan(&task.ID, &task.Title, &task.Description, &task.URL,
		&task.Resource.ID, &task.Resource.Name, &task.Resource.Permission)
	return task, readError(ctx, err)
}

func (p *Provider) Check(ctx context.Context, check ports.AccessCheck) (bool, error) {
	owner, err := principal(ctx)
	if err != nil {
		return false, err
	}
	var granted bool
	err = p.DB.QueryRowContext(ctx, `
 SELECT EXISTS(SELECT 1 FROM demo_external.grants g
 WHERE g.resource_id=r.id AND g.team_id=r.team_id AND g.user_id=$1 AND g.permission=r.permission)
 FROM demo_external.resources r WHERE r.id=$2 AND r.team_id=$3 AND r.permission=$4`,
		owner.UserID, check.ResourceID, owner.TeamID, check.Permission).Scan(&granted)
	return granted, readError(ctx, err)
}

type request struct {
	result                                         ports.AccessResult
	user, team, resource, permission, name, reason string
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadRequest(ctx context.Context, db queryer, id string) (request, error) {
	var r request
	err := db.QueryRowContext(ctx, `SELECT external_id,status,failure_reason,user_id,team_id,
resource_id,permission,resource_name,reason FROM demo_external.access_requests WHERE client_reference=$1`, id).
		Scan(&r.result.ExternalID, &r.result.Status, &r.result.FailureReason, &r.user, &r.team, &r.resource, &r.permission, &r.name, &r.reason)
	return r, err
}

func fault(ctx context.Context, db queryer, id string) (string, error) {
	var mode string
	err := db.QueryRowContext(ctx, `SELECT mode FROM demo_external.faults WHERE client_reference=$1`, id).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return mode, err
}

func (p *Provider) Lookup(ctx context.Context, id string) (ports.AccessResult, error) {
	owner, err := principal(ctx)
	if err != nil {
		return ports.AccessResult{}, err
	}
	r, err := loadRequest(ctx, p.DB, id)
	if err != nil {
		return ports.AccessResult{}, readError(ctx, err)
	}
	if r.user != owner.UserID || r.team != owner.TeamID {
		return ports.AccessResult{}, ports.ErrNotFound
	}
	mode, err := fault(ctx, p.DB, id)
	if err != nil {
		return ports.AccessResult{}, readError(ctx, err)
	}
	if mode == "lookup-unavailable" {
		return ports.AccessResult{}, ports.ErrUnavailable
	}
	return r.result, nil
}

// Submit serializes all attempts for a key, including attempts that arrive late.
// The transaction belongs exclusively to the external simulator.
func (p *Provider) Submit(ctx context.Context, input ports.AccessSubmission) (ports.AccessResult, error) {
	owner, err := principal(ctx)
	if err != nil {
		return ports.AccessResult{}, err
	}
	for _, value := range []string{input.RequestID, input.Resource.ID, input.Resource.Name, input.Resource.Permission, input.Reason} {
		if strings.TrimSpace(value) == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) > 4000 {
			return ports.AccessResult{}, ports.ErrInvalid
		}
	}
	var result ports.AccessResult
	var unknown bool
	err = database.WithinTx(ctx, p.DB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,13))`, input.RequestID); err != nil {
			return err
		}
		r, err := loadRequest(ctx, tx, input.RequestID)
		if err == nil {
			if r.user != owner.UserID || r.team != owner.TeamID {
				return ports.ErrNotFound
			}
			if r.resource != input.Resource.ID || r.permission != input.Resource.Permission || r.name != input.Resource.Name || r.reason != input.Reason {
				return ports.ErrConflict
			}
			result = r.result
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var externalResource, externalRole, name string
		err = tx.QueryRowContext(ctx, `SELECT external_id,external_role,name FROM demo_external.resources
WHERE id=$1 AND team_id=$2 AND permission=$3 FOR SHARE`, input.Resource.ID, owner.TeamID, input.Resource.Permission).
			Scan(&externalResource, &externalRole, &name)
		if err != nil {
			return readError(ctx, err)
		}
		if name != input.Resource.Name {
			return ports.ErrInvalid
		}
		mode, err := fault(ctx, tx, input.RequestID)
		if err != nil {
			return err
		}
		if mode == "reject-before-write" {
			result = ports.AccessResult{Status: "failed", FailureReason: "Тестовый отказ до создания заявки"}
			return nil
		}
		result = ports.AccessResult{ExternalID: "DEMO-" + rand.Text(), Status: "pending"}
		_, err = tx.ExecContext(ctx, `INSERT INTO demo_external.access_requests
(client_reference,external_id,user_id,team_id,resource_id,permission,resource_name,reason,external_resource_id,external_role)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, input.RequestID, result.ExternalID, owner.UserID, owner.TeamID,
			input.Resource.ID, input.Resource.Permission, input.Resource.Name, input.Reason, externalResource, externalRole)
		unknown = mode == "timeout-after-commit"
		return err
	})
	if err != nil {
		for _, known := range []error{ports.ErrNotFound, ports.ErrConflict, ports.ErrInvalid} {
			if errors.Is(err, known) {
				return ports.AccessResult{}, known
			}
		}
		// A failed commit acknowledgement cannot prove that no external row exists.
		return ports.AccessResult{}, ports.ErrOutcomeUnknown
	}
	if unknown {
		return ports.AccessResult{}, ports.ErrOutcomeUnknown
	}
	return result, nil
}
