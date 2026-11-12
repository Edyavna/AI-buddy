// Package onboarding owns plan state and the rules used by both GET and PATCH.
package onboarding

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var (
	ErrTransition  = errors.New("step transition forbidden")
	ErrPlanChanged = errors.New("plan changed during access check")
)

type Step struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Status           string         `json:"status"`
	RequestIDs       []string       `json:"request_ids"`
	AllowedStatuses  []string       `json:"allowed_statuses"`
	BlockedReason    *string        `json:"blocked_reason"`
	TemplateKey      string         `json:"template_key"`
	Period           *string        `json:"period"`
	CompletionPolicy string         `json:"completion_policy"`
	Confirmations    []Confirmation `json:"confirmations"`
	Revision         int64          `json:"revision"`
	AllowedActions   []string       `json:"allowed_actions"`
	AwaitingRole     *identity.Role `json:"awaiting_role"`
	Instructions     string         `json:"instructions"`
	Resources        []Resource     `json:"resources"`
}

type Confirmation struct {
	Role        identity.Role `json:"role"`
	UserID      string        `json:"user_id"`
	ConfirmedAt time.Time     `json:"confirmed_at"`
}

type Resource struct {
	Title string  `json:"title"`
	URL   *string `json:"url"`
	Note  *string `json:"note"`
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

func (snapshot Snapshot) completedSteps() map[string]bool {
	completed := make(map[string]bool)
	for _, r := range snapshot.Steps {
		completed[r.ID] = r.Status == "completed"
	}
	return completed
}

func (r Record) canConfirm(completed map[string]bool) bool {
	return r.Status != "blocked" && prerequisitesMet(r.Prerequisites, completed)
}

func prerequisitesMet(ids []string, completed map[string]bool) bool {
	for _, id := range ids {
		if !completed[id] {
			return false
		}
	}
	return true
}

func (s Step) requires(role identity.Role) bool {
	return s.CompletionPolicy == "both" || s.CompletionPolicy == string(role) ||
		(s.CompletionPolicy == "" && role == identity.Intern)
}

func (s Step) confirmedBy(role identity.Role) string {
	for _, confirmation := range s.Confirmations {
		if confirmation.Role == role {
			return confirmation.UserID
		}
	}
	return ""
}

// project shares progress, dependencies and next-step rules across both roles.
func project(snapshot Snapshot, grants map[ports.AccessCheck]bool, actor identity.Principal) Plan {
	p := Plan{ID: snapshot.ID, Title: snapshot.Title, Steps: make([]Step, 0, len(snapshot.Steps))}
	completed := snapshot.completedSteps()
	for _, r := range snapshot.Steps {
		step := r.Step
		step.RequestIDs = append([]string{}, r.RequestIDs...)
		step.Confirmations = append([]Confirmation{}, r.Confirmations...)
		step.Resources = append([]Resource{}, r.Resources...)
		step.Revision = snapshot.Revision
		step.AllowedStatuses = []string{}
		step.AllowedActions = []string{}
		if step.CompletionPolicy == "both" && len(step.Confirmations) == 1 {
			waiting := identity.Intern
			if step.Confirmations[0].Role == identity.Intern {
				waiting = identity.Mentor
			}
			step.AwaitingRole = &waiting
		}
		canConfirm := r.canConfirm(completed) && (r.Requirement == nil || grants[*r.Requirement])
		if step.Status != "completed" && step.Status != "blocked" && !prerequisitesMet(r.Prerequisites, completed) {
			reason := "Сначала завершите предыдущий шаг"
			step.Status, step.BlockedReason = "blocked", &reason
		}
		if (actor.Role == identity.Intern || actor.Role == identity.Mentor) && step.requires(actor.Role) {
			switch author := step.confirmedBy(actor.Role); {
			case author != "" && author == actor.UserID:
				step.AllowedActions = append(step.AllowedActions, "revoke")
			case author == "" && canConfirm:
				step.AllowedActions = append(step.AllowedActions, "confirm")
			}
		}
		if step.Status != "blocked" {
			step.BlockedReason = nil
		}
		step.AllowedStatuses = legacyStatuses(step, actor, canConfirm)
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

func legacyStatuses(step Step, actor identity.Principal, canConfirm bool) []string {
	statuses := []string{}
	if actor.Role != identity.Intern || step.Status == "blocked" {
		return statuses
	}
	internOnly := step.CompletionPolicy == "intern" || step.CompletionPolicy == ""
	if author := step.confirmedBy(identity.Intern); author != "" && author != actor.UserID {
		return statuses
	}
	if !internOnly && len(step.Confirmations) != 0 {
		return statuses
	}
	for _, status := range []string{"not_started", "in_progress", "completed"} {
		if status != step.Status && (status != "completed" || (internOnly && canConfirm)) {
			statuses = append(statuses, status)
		}
	}
	return statuses
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
	return project(snapshot, grants, p), nil
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
		plan := project(snapshot, grants, p)
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
		return project(updated, grants, p), nil
	}
	// Contention must not be reported as a successful update.
	return Plan{}, ports.ErrUnavailable
}
