// Package onboarding owns plan state and the rules used by both GET and PATCH.
package onboarding

import (
	"context"
	"errors"
	"slices"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var (
	ErrTransition  = errors.New("step transition forbidden")
	ErrPlanChanged = errors.New("plan changed during access check")
)

type Step struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Status          string   `json:"status"`
	RequestIDs      []string `json:"request_ids"`
	AllowedStatuses []string `json:"allowed_statuses"`
	BlockedReason   *string  `json:"blocked_reason"`
}
type Plan struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	ProgressPercent int     `json:"progress_percent"`
	NextStepID      *string `json:"next_step_id"`
	Steps           []Step  `json:"steps"`
}

// Record is a persisted step; derived UI fields are never accepted from HTTP.
type Record struct {
	Step
	Requirement   *ports.AccessCheck
	Prerequisites []string
}
type Snapshot struct {
	ID, Title string
	Revision  int64
	Steps     []Record
}
type Store interface {
	Read(context.Context, identity.Principal) (Snapshot, error)
	// Patch serializes with request-service on the parent plan, and only writes
	// if the snapshot checked by Service still has the expected revision.
	Patch(context.Context, identity.Principal, int64, string, string) (Snapshot, error)
}
type Service struct {
	Store  Store
	Access ports.AccessChecker
}

func principal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return p, err
	}
	if p.Role != identity.Intern {
		return p, ports.ErrForbidden
	}
	return p, nil
}
func (s Service) grants(ctx context.Context, snapshot Snapshot) (map[ports.AccessCheck]bool, error) {
	result := make(map[ports.AccessCheck]bool)
	for _, step := range snapshot.Steps {
		if step.Requirement == nil {
			continue
		}
		key := *step.Requirement
		if _, ok := result[key]; ok {
			continue
		}
		if s.Access == nil {
			return nil, ports.ErrUnavailable
		}
		allowed, err := s.Access.Check(ctx, key)
		if err != nil {
			return nil, err
		}
		result[key] = allowed
	}
	return result, nil
}

// project is pure: one policy for progress, dependencies and allowed transitions.
// A missing grant forbids completion but does not block asking for help. Explicit
// request blocks and unfinished prerequisites prohibit every user transition.
func project(snapshot Snapshot, grants map[ports.AccessCheck]bool) Plan {
	p := Plan{ID: snapshot.ID, Title: snapshot.Title, Steps: make([]Step, 0, len(snapshot.Steps))}
	completed := make(map[string]bool)
	for _, r := range snapshot.Steps {
		completed[r.ID] = r.Status == "completed"
	}
	for _, r := range snapshot.Steps {
		step := r.Step
		step.RequestIDs = slices.Clone(r.RequestIDs)
		if step.RequestIDs == nil {
			step.RequestIDs = []string{}
		}
		step.AllowedStatuses = []string{}
		if step.Status != "completed" && step.Status != "blocked" {
			for _, id := range r.Prerequisites {
				if !completed[id] {
					reason := "Сначала завершите предыдущий шаг"
					step.Status, step.BlockedReason = "blocked", &reason
					break
				}
			}
		}
		if step.Status != "blocked" {
			step.BlockedReason = nil
			// Any user status can be revised; completion additionally needs access.
			for _, status := range []string{"not_started", "in_progress", "completed"} {
				if status == step.Status {
					continue
				}
				if status == "completed" && r.Requirement != nil && !grants[*r.Requirement] {
					continue
				}
				step.AllowedStatuses = append(step.AllowedStatuses, status)
			}
		}
		if step.Status == "completed" {
			p.ProgressPercent++
		}
		if p.NextStepID == nil && step.Status != "completed" && step.Status != "blocked" {
			id := step.ID
			p.NextStepID = &id
		}
		p.Steps = append(p.Steps, step)
	}
	if len(p.Steps) > 0 {
		p.ProgressPercent = 100 * p.ProgressPercent / len(p.Steps)
	}
	return p
}
func (s Service) Get(ctx context.Context) (Plan, error) {
	p, err := principal(ctx)
	if err != nil {
		return Plan{}, err
	}
	snapshot, err := s.Store.Read(ctx, p)
	if err != nil {
		return Plan{}, err
	}
	grants, err := s.grants(ctx, snapshot)
	if err != nil {
		return Plan{}, err
	}
	return project(snapshot, grants), nil
}
func (s Service) Update(ctx context.Context, id, status string) (Plan, error) {
	p, err := principal(ctx)
	if err != nil {
		return Plan{}, err
	}
	for range 3 {
		snapshot, err := s.Store.Read(ctx, p)
		if err != nil {
			return Plan{}, err
		}
		found := false
		for _, step := range snapshot.Steps {
			if step.ID == id {
				found = true
				break
			}
		}
		if !found {
			return Plan{}, ports.ErrNotFound
		}
		if !slices.Contains([]string{"not_started", "in_progress", "completed"}, status) {
			return Plan{}, ports.ErrInvalid
		}
		grants, err := s.grants(ctx, snapshot)
		if err != nil {
			return Plan{}, err
		}
		plan := project(snapshot, grants)
		for _, step := range plan.Steps {
			if step.ID != id {
				continue
			}
			if step.Status != status && !slices.Contains(step.AllowedStatuses, status) {
				return Plan{}, ErrTransition
			}
		}
		updated, err := s.Store.Patch(ctx, p, snapshot.Revision, id, status)
		if errors.Is(err, ErrPlanChanged) {
			continue
		}
		if err != nil {
			return Plan{}, err
		}
		return project(updated, grants), nil
	}
	// Contention must not be reported as a successful update.
	return Plan{}, ports.ErrUnavailable
}
