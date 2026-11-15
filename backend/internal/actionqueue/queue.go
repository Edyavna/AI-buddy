package actionqueue

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Item struct {
	Kind      string    `json:"kind"`
	ID        string    `json:"id"`
	Intern    Person    `json:"intern"`
	StepID    *string   `json:"step_id"`
	StepTitle *string   `json:"step_title"`
	Question  *string   `json:"question_preview"`
	Action    string    `json:"action"`
	Since     time.Time `json:"since"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Page struct {
	Items   []Item         `json:"items"`
	Interns []Person       `json:"interns"`
	Counts  map[string]int `json:"counts"`
	Total   int            `json:"total"`
}

const sourceItems = `
WITH assigned AS (
    SELECT u.id,u.name,u.team_id FROM mentor_assignments a
    JOIN users u ON u.id=a.intern_id AND u.team_id=a.team_id AND u.role='intern'
    JOIN users m ON m.id=a.mentor_id AND m.team_id=a.team_id AND m.role='mentor'
    WHERE $3='mentor' AND a.mentor_id=$1 AND a.team_id=$2
)
SELECT 'invitation',i.id::text,u.id,u.name,s.id,s.title,NULL::text,'record_invitation',i.requested_at,i.requested_at
FROM assigned u JOIN onboarding_plans p ON p.user_id=u.id AND p.team_id=u.team_id
JOIN onboarding_steps s ON s.plan_id=p.id
JOIN checkpoint_invitation_requests i ON i.step_id=s.id AND i.state='pending'
UNION ALL
SELECT 'confirmation',s.id,u.id,u.name,s.id,s.title,NULL::text,'review_checkpoint',
       GREATEST(s.status_changed_at,c.confirmed_at,
           (SELECT max(d.status_changed_at) FROM onboarding_step_dependencies r
            JOIN onboarding_steps d ON d.id=r.prerequisite_id WHERE r.step_id=s.id)),
       GREATEST(s.status_changed_at,c.confirmed_at,
           (SELECT max(d.status_changed_at) FROM onboarding_step_dependencies r
            JOIN onboarding_steps d ON d.id=r.prerequisite_id WHERE r.step_id=s.id))
FROM assigned u JOIN onboarding_plans p ON p.user_id=u.id AND p.team_id=u.team_id
JOIN onboarding_steps s ON s.plan_id=p.id
LEFT JOIN onboarding_step_confirmations c ON c.step_id=s.id AND c.role='intern'
WHERE (s.completion_policy='mentor' OR (s.completion_policy='both' AND c.step_id IS NOT NULL))
  AND NOT EXISTS (SELECT 1 FROM onboarding_step_confirmations m WHERE m.step_id=s.id AND m.role='mentor')
UNION ALL
SELECT 'question',q.id::text,u.id,u.name,s.id,s.title,left(q.question,160),
       CASE WHEN q.direction='hr' AND q.status='new' THEN 'claim_question' ELSE 'answer_question' END,
       q.waiting_since,q.updated_at
FROM knowledge_gaps q JOIN users u ON u.id=q.student_id AND u.team_id=q.team_id AND u.role='intern'
LEFT JOIN onboarding_plans p ON p.user_id=u.id AND p.team_id=u.team_id
LEFT JOIN onboarding_steps s ON s.id=q.step_id AND s.plan_id=p.id
WHERE q.direction=$3 AND q.status IN ('new','in_progress')
AND (($3='mentor' AND EXISTS(SELECT 1 FROM assigned a WHERE a.id=u.id))
  OR ($3='hr' AND (q.hr_id IS NULL OR q.hr_id=$1) AND EXISTS(
      SELECT 1 FROM hr_team_assignments h WHERE h.hr_user_id=$1 AND h.active AND h.team_id=q.team_id)))
`

func (s Store) items(ctx context.Context, actor identity.Principal) ([]Item, error) {
	rows, err := s.DB.QueryContext(ctx, sourceItems, actor.UserID, actor.TeamID, actor.Role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.Kind, &item.ID, &item.Intern.ID, &item.Intern.Name, &item.StepID, &item.StepTitle, &item.Question,
			&item.Action, &item.Since, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Since, item.UpdatedAt = item.Since.UTC(), item.UpdatedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

// The queue reports a provider outage instead of marking unverified steps actionable.
type trackedAccess struct {
	ports.AccessChecker
	err error
}

func (a *trackedAccess) Check(ctx context.Context, access ports.AccessCheck) (bool, error) {
	if a.AccessChecker == nil {
		a.err = ports.ErrUnavailable
		return false, a.err
	}
	allowed, err := a.AccessChecker.Check(ctx, access)
	if err != nil {
		a.err = err
	}
	return allowed, err
}

func (s Store) confirmable(ctx context.Context, actor identity.Principal, intern string) (map[string]bool, error) {
	var owner identity.Principal
	var snapshot onboarding.Snapshot
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		owner, err = s.Checkpoints.Assignments.AssignedInternTx(ctx, tx, intern)
		if err != nil {
			return err
		}
		snapshot, err = onboarding.ReadSnapshotTx(ctx, tx, owner)
		return err
	})
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	access := &trackedAccess{AccessChecker: s.Checkpoints.Access}
	checkpoints := onboarding.Checkpoints{Access: access}
	result := make(map[string]bool)
	for _, step := range checkpoints.View(ctx, snapshot, owner, actor).Steps {
		result[step.ID] = slices.Contains(step.AllowedActions, "confirm") &&
			(step.CompletionPolicy == "mentor" || (step.AwaitingRole != nil && *step.AwaitingRole == identity.Mentor))
	}
	return result, access.err
}

func (s Store) List(ctx context.Context, intern, kind string, offset, limit int) (Page, error) {
	actor, err := requireRole(ctx, identity.Mentor, identity.HR)
	if err != nil {
		return Page{}, err
	}
	items, err := s.items(ctx, actor)
	if err != nil {
		return Page{}, storeError(err)
	}
	slices.SortFunc(items, func(a, b Item) int {
		return cmp.Or(a.Since.Compare(b.Since), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID))
	})
	page := Page{Items: []Item{}, Interns: []Person{}, Counts: map[string]int{"invitation": 0, "confirmation": 0, "question": 0}}
	confirmed := make(map[string]map[string]bool)
	people := make(map[string]Person)
	for _, item := range items {
		id := item.Intern.ID
		if item.Kind == "confirmation" {
			if _, loaded := confirmed[id]; !loaded {
				confirmed[id], err = s.confirmable(ctx, actor, id)
				if err != nil {
					return Page{}, storeError(err)
				}
			}
			if !confirmed[id][item.ID] {
				continue
			}
		}
		people[id] = item.Intern
		if intern != "" && intern != id {
			continue
		}
		page.Counts[item.Kind]++
		if kind != "" && kind != item.Kind {
			continue
		}
		if page.Total >= offset && len(page.Items) < limit {
			page.Items = append(page.Items, item)
		}
		page.Total++
	}
	for _, person := range people {
		page.Interns = append(page.Interns, person)
	}
	slices.SortFunc(page.Interns, func(a, b Person) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return page, nil
}
