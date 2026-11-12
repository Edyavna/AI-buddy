package mentor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Store struct{ DB *sql.DB }

var _ ports.MentorAssignments = Store{}

type assignment struct {
	owner identity.Principal
	name  string
	team  auth.Team
}

func mentorPrincipal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err == nil && p.Role != identity.Mentor {
		err = ports.ErrForbidden
	}
	return p, err
}

func storeError(err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ports.ErrNotFound
	case err == nil, errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrUnavailable), errors.Is(err, ErrAlreadyAssigned):
		return err
	default:
		return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
	}
}

func (s Store) AssignedMentor(ctx context.Context) (identity.Principal, error) {
	intern, err := identity.Require(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	if intern.Role != identity.Intern {
		return identity.Principal{}, ports.ErrForbidden
	}
	result := identity.Principal{TeamID: intern.TeamID, Role: identity.Mentor}
	err = s.DB.QueryRowContext(ctx, `SELECT a.mentor_id
FROM mentor_assignments a
JOIN users u ON u.id=a.intern_id AND u.team_id=a.team_id AND u.role='intern'
JOIN users m ON m.id=a.mentor_id AND m.team_id=a.team_id AND m.role='mentor'
WHERE a.intern_id=$1 AND a.team_id=$2`, intern.UserID, intern.TeamID).Scan(&result.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Principal{}, ports.ErrMentorUnassigned
	}
	if err != nil {
		return identity.Principal{}, storeError(err)
	}
	return result, nil
}

func (s Store) CheckMentor(ctx context.Context, internID string) error {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return err
	}
	if internID == "" {
		return ports.ErrNotFound
	}
	return storeError(database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		_, err := assigned(ctx, tx, p, internID)
		return err
	}))
}

func assigned(ctx context.Context, tx *sql.Tx, p identity.Principal, internID string) ([]assignment, error) {
	rows, err := tx.QueryContext(ctx, `SELECT u.id,u.name,t.id,t.name
FROM mentor_assignments a
JOIN users u ON u.id=a.intern_id AND u.team_id=a.team_id AND u.role='intern'
JOIN users m ON m.id=a.mentor_id AND m.team_id=a.team_id AND m.role='mentor'
JOIN teams t ON t.id=a.team_id
WHERE a.mentor_id=$1 AND a.team_id=$2 AND ($3='' OR a.intern_id=$3)
ORDER BY u.id FOR SHARE OF a,u,m`, p.UserID, p.TeamID, internID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []assignment
	for rows.Next() {
		var item assignment
		if err := rows.Scan(&item.owner.UserID, &item.name, &item.team.ID, &item.team.Name); err != nil {
			return nil, err
		}
		item.owner.TeamID, item.owner.Role = item.team.ID, identity.Intern
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if internID != "" && len(result) == 0 {
		return nil, ports.ErrNotFound
	}
	return result, nil
}

func (s Store) Plan(ctx context.Context, internID string) (onboarding.Plan, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return onboarding.Plan{}, err
	}
	if internID == "" {
		return onboarding.Plan{}, ports.ErrNotFound
	}
	var plan onboarding.Plan
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		items, err := assigned(ctx, tx, p, internID)
		if err != nil {
			return err
		}
		plan, err = onboarding.ReadOnlyPlanTx(ctx, tx, items[0].owner)
		return err
	})
	return plan, storeError(err)
}

type InternSummary struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Team            auth.Team `json:"team"`
	ProgressPercent int       `json:"progress_percent"`
	NextStepID      *string   `json:"next_step_id"`
	CompletedSteps  int       `json:"completed_steps"`
	TotalSteps      int       `json:"total_steps"`
	BlockedSteps    int       `json:"blocked_steps"`
	OpenQuestions   *int      `json:"open_questions"`
}

func (s Store) List(ctx context.Context, counter ports.OpenQuestionCounter) ([]InternSummary, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	result := []InternSummary{}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		items, err := assigned(ctx, tx, p, "")
		if err != nil {
			return err
		}
		for _, item := range items {
			plan, err := onboarding.ReadOnlyPlanTx(ctx, tx, item.owner)
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return err
			}
			summary := InternSummary{
				ID: item.owner.UserID, Name: item.name, Team: item.team, ProgressPercent: plan.ProgressPercent,
				NextStepID: plan.NextStepID, TotalSteps: len(plan.Steps),
			}
			for _, step := range plan.Steps {
				switch step.Status {
				case "completed":
					summary.CompletedSteps++
				case "blocked":
					summary.BlockedSteps++
				}
			}
			result = append(result, summary)
		}
		return nil
	})
	if err != nil {
		return nil, storeError(err)
	}
	if counter != nil {
		for i := range result {
			count, err := counter.CountOpen(ctx, result[i].ID)
			if err != nil {
				return nil, err
			}
			result[i].OpenQuestions = &count
		}
	}
	return result, nil
}
