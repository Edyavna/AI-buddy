package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/mentor"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type questionCountFixture struct{}

func (questionCountFixture) CountOpen(ctx context.Context, internID string) (int, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return 0, err
	}
	if p.Role != identity.Mentor || p.UserID != "mentor_demo_1" || internID != testkit.UserID {
		return 0, ports.ErrNotFound
	}
	return 2, nil
}

func TestPostgresMentorAssignmentAndReadOnlyPlan(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	for range 2 {
		if err := demo.Seed(ctx, db, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE onboarding_steps SET status='completed' WHERE id='step_local_setup';
INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,blocked_reason) VALUES
('step_blocked','plan_demo_1',1,'Заблокированный шаг','Демо','blocked','Нужен доступ')`); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	const base = "/api/v1/mentor/interns"
	const ownPlan = base + "/intern_demo_1/plan"
	apiRequest(t, h, "GET", base, "", nil, 401)
	apiRequest(t, h, "GET", ownPlan, "", nil, 401)
	intern := loginCookie(t, h, testkit.UserID, nil)
	apiRequest(t, h, "GET", base, "", intern, 403)
	apiRequest(t, h, "GET", ownPlan, "", intern, 403)
	first := loginCookie(t, h, "mentor_demo_1", nil)
	second := loginCookie(t, h, "mentor_demo_2", nil)
	apiRequest(t, h, "GET", ownPlan, "", second, 404)
	apiRequest(t, h, "GET", base+"/intern_demo_other/plan", "", first, 404)
	apiRequest(t, h, "GET", planURL, "", first, 403)
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, first, 403)

	w := apiRequest(t, h, "GET", base, "", first, 200)
	var list struct{ Items []mentor.InternSummary }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatal(err, w.Body.String())
	}
	got := list.Items[0]
	if got.ID != testkit.UserID || got.ProgressPercent != 50 || got.CompletedSteps != 1 ||
		got.BlockedSteps != 1 || got.TotalSteps != 2 || got.NextStepID != nil || got.OpenQuestions == nil || *got.OpenQuestions != 0 {
		t.Fatal(got)
	}

	studentPlan := decodePlan(t, apiRequest(t, h, "GET", planURL, "", intern, 200))
	mentorPlan := decodePlan(t, apiRequest(t, h, "GET", ownPlan, "", first, 200))
	if mentorPlan.ID != studentPlan.ID || mentorPlan.ProgressPercent != studentPlan.ProgressPercent ||
		len(mentorPlan.Steps) != len(studentPlan.Steps) {
		t.Fatal(studentPlan, mentorPlan)
	}
	for i, step := range mentorPlan.Steps {
		if step.ID != studentPlan.Steps[i].ID || step.Status != studentPlan.Steps[i].Status ||
			len(step.AllowedStatuses) != 0 {
			t.Fatal(step)
		}
	}

	assignments := mentor.Store{DB: db}
	assigned, err := assignments.AssignedMentor(testkit.Context(ctx))
	if err != nil || assigned.UserID != "mentor_demo_1" {
		t.Fatal(assigned, err)
	}
	if err := assignments.CheckMentor(identity.WithPrincipal(ctx, assigned), testkit.UserID); err != nil {
		t.Fatal(err)
	}
	unassigned := identity.WithPrincipal(ctx, identity.Principal{
		UserID: "intern_demo_other", TeamID: "team_demo_frontend", Role: identity.Intern,
	})
	if _, err := assignments.AssignedMentor(unassigned); !errors.Is(err, ports.ErrMentorUnassigned) {
		t.Fatal(err)
	}
	h = requestHandler(db, Dependencies{Questions: questionCountFixture{}})
	w = apiRequest(t, h, "GET", base, "", first, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 ||
		list.Items[0].OpenQuestions == nil || *list.Items[0].OpenQuestions != 2 {
		t.Fatal(err, w.Body.String())
	}
}
