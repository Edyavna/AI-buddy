package onboarding

import (
	"context"
	"database/sql"
	"slices"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// CheckpointContext contains the facts the agent can use about an intern's step.
type CheckpointContext struct {
	Title         string             `json:"title"`
	Instructions  string             `json:"instructions"`
	Period        *string            `json:"period"`
	Resources     []Resource         `json:"resources"`
	Status        string             `json:"status"`
	Policy        string             `json:"completion_policy"`
	Missing       []identity.Role    `json:"missing_confirmations"`
	Prerequisites []string           `json:"unfinished_prerequisites"`
	BlockedReason *string            `json:"blocked_reason"`
	ManagedAccess *ports.AccessCheck `json:"-"`
}

func (s SQLStore) Checkpoint(ctx context.Context, id string) (CheckpointContext, error) {
	owner, err := principal(ctx)
	if err != nil {
		return CheckpointContext{}, err
	}
	var result CheckpointContext
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := checkInternTx(ctx, tx, owner); err != nil {
			return err
		}
		snapshot, err := ReadSnapshotTx(ctx, tx, owner)
		if err != nil {
			return err
		}
		step, err := findStep(snapshot, id)
		if err != nil {
			return err
		}
		completed := snapshot.completedSteps()
		status, blockedReason := step.effectiveStatus(completed)
		result = CheckpointContext{
			Title: step.Title, Instructions: step.Instructions, Period: step.Period,
			Resources: step.Resources, Policy: step.CompletionPolicy, Status: status,
			BlockedReason: blockedReason, ManagedAccess: step.Requirement,
			Missing: []identity.Role{}, Prerequisites: []string{},
		}
		for _, role := range []identity.Role{identity.Intern, identity.Mentor} {
			if step.requires(role) && step.confirmedBy(role) == "" {
				result.Missing = append(result.Missing, role)
			}
		}
		for _, candidate := range snapshot.Steps {
			if !completed[candidate.ID] && slices.Contains(step.Prerequisites, candidate.ID) {
				result.Prerequisites = append(result.Prerequisites, candidate.Title)
			}
		}
		return nil
	})
	return result, sqlError(err)
}
