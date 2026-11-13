package onboarding

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Assignments interface {
	AssignedInternTx(context.Context, *sql.Tx, string) (identity.Principal, error)
}

type Checkpoints struct {
	DB          *sql.DB
	Access      ports.AccessChecker
	Assignments Assignments
}

type SetConfirmation struct {
	Confirmed bool
	Revision  int64
}

var errCheckGrant = errors.New("checkpoint requires an access check")

// Set applies the authenticated actor's desired state. The route supplies role;
// the mentor's internID is used only for the transactional assignment lookup.
func (s Checkpoints) Set(ctx context.Context, role identity.Role, internID, stepID string, change SetConfirmation) (Plan, error) {
	actor, err := identity.Require(ctx)
	if err != nil {
		return Plan{}, err
	}
	if actor.Role != role {
		return Plan{}, ports.ErrForbidden
	}
	if role == identity.Mentor && s.Assignments == nil {
		return Plan{}, ports.ErrUnavailable
	}
	owner := actor
	var snapshot Snapshot
	var required ports.AccessCheck
	grants := make(map[ports.AccessCheck]bool)

	apply := func() error {
		return database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
			if role == identity.Mentor {
				var err error
				owner, err = s.Assignments.AssignedInternTx(ctx, tx, internID)
				if err != nil {
					return err
				}
			} else if err := checkInternTx(ctx, tx, actor); err != nil {
				return err
			}
			var err error
			snapshot, err = lockPlan(ctx, tx, owner, true)
			if err != nil {
				return err
			}
			if err := readSteps(ctx, tx, &snapshot); err != nil {
				return err
			}
			step, err := findStep(snapshot, stepID)
			if err != nil {
				return err
			}
			author := step.confirmedBy(actor.Role)
			if !step.requires(actor.Role) || (author != "" && author != actor.UserID) {
				return ports.ErrForbidden
			}
			if change.Confirmed == (author != "") {
				return nil
			}
			if change.Revision != snapshot.Revision {
				return ErrPlanChanged
			}
			if change.Confirmed {
				if !step.canConfirm(snapshot.completedSteps()) {
					return ErrTransition
				}
				if step.Requirement != nil {
					required = *step.Requirement
					allowed, checked := grants[required]
					if !checked {
						return errCheckGrant
					}
					if !allowed {
						return ErrTransition
					}
				}
			}

			if step.Status != "blocked" {
				status := "in_progress"
				if change.Confirmed && (step.CompletionPolicy != "both" || len(step.Confirmations) == 1) {
					status = "completed"
				}
				if err := saveStepStatus(ctx, tx, step.ID, status); err != nil {
					return err
				}
			}
			if err := writeConfirmation(ctx, tx, step.ID, actor, change.Confirmed); err != nil {
				return err
			}
			if err := advanceRevision(ctx, tx, &snapshot); err != nil {
				return err
			}
			return readSteps(ctx, tx, &snapshot)
		})
	}

	if err = apply(); errors.Is(err, errCheckGrant) {
		// The access provider can do network or pool I/O. Release the transaction,
		// then revalidate the assignment, plan revision and dependencies on write.
		if s.Access == nil {
			return Plan{}, ports.ErrUnavailable
		}
		allowed, checkErr := s.Access.Check(identity.WithPrincipal(ctx, owner), required)
		if checkErr != nil {
			return Plan{}, checkErr
		}
		grants[required] = allowed
		err = apply()
	}
	if err != nil {
		return Plan{}, sqlError(err)
	}
	return s.view(ctx, snapshot, owner, actor, grants), nil
}

func writeConfirmation(ctx context.Context, tx *sql.Tx, stepID string, actor identity.Principal, confirmed bool) error {
	if confirmed {
		_, err := tx.ExecContext(ctx, `INSERT INTO onboarding_step_confirmations(step_id,role,user_id) VALUES($1,$2,$3)`,
			stepID, actor.Role, actor.UserID)
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM onboarding_step_confirmations WHERE step_id=$1 AND role=$2 AND user_id=$3`,
		stepID, actor.Role, actor.UserID)
	return err
}

// View projects an authorized snapshot for the viewer, using the plan owner's
// access rights. Provider failures suppress the affected action.
func (s Checkpoints) View(ctx context.Context, snapshot Snapshot, owner, actor identity.Principal) Plan {
	return s.view(ctx, snapshot, owner, actor, make(map[ports.AccessCheck]bool))
}

func (s Checkpoints) view(ctx context.Context, snapshot Snapshot, owner, actor identity.Principal, grants map[ports.AccessCheck]bool) Plan {
	if s.Access != nil {
		ownerContext := identity.WithPrincipal(ctx, owner)
		completed := snapshot.completedSteps()
		for _, step := range snapshot.Steps {
			if step.Requirement == nil || !step.requires(actor.Role) || step.confirmedBy(actor.Role) != "" || !step.canConfirm(completed) {
				continue
			}
			if _, checked := grants[*step.Requirement]; !checked {
				allowed, err := s.Access.Check(ownerContext, *step.Requirement)
				grants[*step.Requirement] = err == nil && allowed
			}
		}
	}
	return project(snapshot, grants, actor)
}
