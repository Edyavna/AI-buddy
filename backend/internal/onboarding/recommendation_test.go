package onboarding

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func TestRecommendationScenarios(t *testing.T) {
	actor := identity.Principal{UserID: "intern_1", TeamID: "team_1", Role: identity.Intern}
	now := time.Now().Add(-10 * time.Minute)

	t.Run("empty plan returns empty_plan state and null next_step_id", func(t *testing.T) {
		snap := Snapshot{ID: "plan_empty", Title: "Пустой план", Revision: 1, Steps: []Record{}, HasHR: true}
		plan := project(snap, nil, actor)

		if plan.NextStepID != nil {
			t.Fatalf("expected nil next_step_id, got %v", *plan.NextStepID)
		}
		if plan.Recommendation.State != StateEmptyPlan {
			t.Fatalf("expected empty_plan state, got %s", plan.Recommendation.State)
		}
	})

	t.Run("100 percent completed plan returns completed state and null next_step_id", func(t *testing.T) {
		snap := Snapshot{
			ID: "plan_done", Title: "Завершённый план", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_1", Title: "Шаг 1", Status: "completed", CompletionPolicy: "intern",
						Confirmations: []Confirmation{{Role: identity.Intern, UserID: "intern_1"}},
					},
				},
			},
		}
		plan := project(snap, nil, actor)

		if plan.NextStepID != nil {
			t.Fatalf("expected nil next_step_id, got %v", *plan.NextStepID)
		}
		if plan.Recommendation.State != StateCompleted {
			t.Fatalf("expected completed state, got %s", plan.Recommendation.State)
		}
		if plan.ProgressPercent != 100 {
			t.Fatalf("expected 100%%, got %d", plan.ProgressPercent)
		}
	})

	t.Run("first step actionable: recommended directly with consistent next_step_id", func(t *testing.T) {
		snap := Snapshot{
			ID: "plan_first", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_1", Title: "Шаг 1", Status: "in_progress", CompletionPolicy: "intern",
						Instructions: "Инструкция к шагу 1",
					},
				},
				{
					Step: Step{
						ID: "step_2", Title: "Шаг 2", Status: "not_started", CompletionPolicy: "intern",
					},
				},
			},
		}
		plan := project(snap, nil, actor)

		if plan.NextStepID == nil || *plan.NextStepID != "step_1" {
			t.Fatalf("expected step_1, got %v", plan.NextStepID)
		}
		rec := plan.Recommendation
		if rec.State != StateActionAvailable || *rec.StepID != "step_1" {
			t.Fatalf("unexpected recommendation: %+v", rec)
		}
		if rec.ActionType == nil || *rec.ActionType != ActionReadInstructions {
			t.Fatalf("expected read_instructions action, got %v", rec.ActionType)
		}
		if rec.WaitingStep != nil {
			t.Fatalf("expected nil waiting_step, got %+v", rec.WaitingStep)
		}
	})

	t.Run("first step blocked by access, second independent: recommend second and show priority waiting", func(t *testing.T) {
		req := ports.AccessCheck{ResourceID: "repo:code", Permission: "write"}
		reason := "Ожидается доступ к repo:code"
		snap := Snapshot{
			ID: "plan_acc", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Requirement: &req,
					BlockedAt:   &now,
					Step: Step{
						ID: "step_1", Title: "Доступ к коду", Status: "blocked", BlockedReason: &reason,
						CompletionPolicy: "intern",
					},
				},
				{
					Step: Step{
						ID: "step_2", Title: "Организационные вопросы", Status: "not_started", CompletionPolicy: "intern",
						Instructions: "Познакомьтесь с командой",
					},
				},
			},
		}
		grants := map[ports.AccessCheck]bool{req: false}
		plan := project(snap, grants, actor)

		// next_step_id must point to step_2
		if plan.NextStepID == nil || *plan.NextStepID != "step_2" {
			t.Fatalf("expected step_2, got %v", plan.NextStepID)
		}
		rec := plan.Recommendation
		if rec.State != StateActionAvailable || *rec.StepID != "step_2" {
			t.Fatalf("expected step_2 recommended, got %+v", rec)
		}
		if rec.WaitingStep == nil {
			t.Fatal("expected waiting_step to describe priority step_1")
		}
		if rec.WaitingStep.StepID != "step_1" || rec.WaitingStep.State != StateWaitingAccess {
			t.Fatalf("unexpected waiting step: %+v", rec.WaitingStep)
		}
		if rec.WaitingStep.BlockedAt == nil || rec.WaitingStep.WaitingDurationSeconds == nil || *rec.WaitingStep.WaitingDurationSeconds <= 0 {
			t.Fatalf("expected positive duration, got %+v", rec.WaitingStep)
		}
	})

	t.Run("first step blocked, second depends on first: no available action and returns waiting state", func(t *testing.T) {
		reason := "Сначала завершите шаг 1"
		snap := Snapshot{
			ID: "plan_dep", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_1", Title: "Шаг 1", Status: "blocked", BlockedReason: &reason,
						CompletionPolicy: "intern",
					},
				},
				{
					Prerequisites: []string{"step_1"},
					Step: Step{
						ID: "step_2", Title: "Шаг 2", Status: "not_started", CompletionPolicy: "intern",
					},
				},
			},
		}
		plan := project(snap, nil, actor)

		if plan.NextStepID != nil {
			t.Fatalf("expected nil next_step_id when all steps blocked, got %v", *plan.NextStepID)
		}
		rec := plan.Recommendation
		if rec.State != StateBlockedDeps && rec.State != StateBlocked {
			t.Fatalf("expected blocked state, got %s", rec.State)
		}
		if rec.HelpAction == nil || rec.HelpAction.Direction != "mentor" {
			t.Fatalf("expected mentor help action, got %+v", rec.HelpAction)
		}
	})

	t.Run("both policy with intern mark awaits mentor and recommends next independent step", func(t *testing.T) {
		snap := Snapshot{
			ID: "plan_both", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_1", Title: "1:1 с наставником", Status: "in_progress", CompletionPolicy: "both",
						Confirmations: []Confirmation{{Role: identity.Intern, UserID: "intern_1", ConfirmedAt: now}},
					},
				},
				{
					Step: Step{
						ID: "step_2", Title: "Прочитать документацию", Status: "not_started", CompletionPolicy: "intern",
						Instructions: "Документация проекта",
					},
				},
			},
		}
		plan := project(snap, nil, actor)

		// intern already confirmed step 1, so step 2 should be recommended!
		if plan.NextStepID == nil || *plan.NextStepID != "step_2" {
			t.Fatalf("expected step_2, got %v", plan.NextStepID)
		}
		rec := plan.Recommendation
		if rec.State != StateActionAvailable || *rec.StepID != "step_2" {
			t.Fatalf("expected step_2 recommended, got %+v", rec)
		}
		if rec.WaitingStep == nil || rec.WaitingStep.StepID != "step_1" || rec.WaitingStep.State != StateWaitingMentor {
			t.Fatalf("expected step_1 waiting_mentor, got %+v", rec.WaitingStep)
		}
	})

	t.Run("both policy without intern mark recommends step 1 directly", func(t *testing.T) {
		snap := Snapshot{
			ID: "plan_both_new", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_1", Title: "1:1 с наставником", Status: "not_started", CompletionPolicy: "both",
						Confirmations: []Confirmation{},
					},
				},
			},
		}
		plan := project(snap, nil, actor)

		if plan.NextStepID == nil || *plan.NextStepID != "step_1" {
			t.Fatalf("expected step_1, got %v", plan.NextStepID)
		}
		rec := plan.Recommendation
		if rec.State != StateActionAvailable || *rec.StepID != "step_1" {
			t.Fatalf("expected step_1, got %+v", rec)
		}
		if !slices.Contains(plan.Steps[0].AllowedActions, "confirm") {
			t.Fatalf("expected intern can confirm, got %v", plan.Steps[0].AllowedActions)
		}
	})

	t.Run("legacy step without history handles null blocked_at and null duration gracefully", func(t *testing.T) {
		reason := "Старая блокировка"
		snap := Snapshot{
			ID: "plan_legacy", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					BlockedAt: nil, // old step without history!
					Step: Step{
						ID: "step_old", Title: "Старый шаг", Status: "blocked", BlockedReason: &reason,
						CompletionPolicy: "intern",
					},
				},
			},
		}
		plan := project(snap, nil, actor)

		rec := plan.Recommendation
		if rec.WaitingStep == nil {
			t.Fatal("expected waiting_step")
		}
		if rec.WaitingStep.BlockedAt != nil || rec.WaitingStep.WaitingDurationSeconds != nil {
			t.Fatalf("expected nil blocked_at and duration for legacy step, got %+v", rec.WaitingStep)
		}
	})

	t.Run("HR integration: HR step routes help to HR, unavailable if team has no HR", func(t *testing.T) {
		reason := "Ожидается согласование кадровых документов"
		// Team with HR:
		snapWithHR := Snapshot{
			ID: "plan_hr_yes", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_hr", Title: "Оформление отпуска и кадровые документы", Status: "blocked",
						TemplateKey: "hr_vacation", BlockedReason: &reason, CompletionPolicy: "intern",
					},
				},
			},
		}
		plan1 := project(snapWithHR, nil, actor)
		if plan1.Recommendation.HelpAction == nil || plan1.Recommendation.HelpAction.Direction != "hr" || !plan1.Recommendation.HelpAction.Available {
			t.Fatalf("expected available HR help action, got %+v", plan1.Recommendation.HelpAction)
		}

		// Team WITHOUT HR:
		snapNoHR := Snapshot{
			ID: "plan_hr_no", Title: "План", Revision: 1, HasHR: false,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_hr", Title: "Оформление отпуска и кадровые документы", Status: "blocked",
						TemplateKey: "hr_vacation", BlockedReason: &reason, CompletionPolicy: "intern",
					},
				},
			},
		}
		plan2 := project(snapNoHR, nil, actor)
		if plan2.Recommendation.HelpAction == nil || plan2.Recommendation.HelpAction.Direction != "hr" || plan2.Recommendation.HelpAction.Available {
			t.Fatalf("expected unavailable HR help action for team without HR, got %+v", plan2.Recommendation.HelpAction)
		}
	})

	t.Run("assistant action when instructions mention assistant or chat", func(t *testing.T) {
		snap := Snapshot{
			ID: "plan_chat", Title: "План", Revision: 1, HasHR: true,
			Steps: []Record{
				{
					Step: Step{
						ID: "step_bot", Title: "Уточнить у помощника", Status: "not_started",
						CompletionPolicy: "intern", Instructions: "Задай вопрос в чат помощнику о допусках",
					},
				},
			},
		}
		plan := project(snap, nil, actor)
		rec := plan.Recommendation
		if rec.ActionType == nil || *rec.ActionType != ActionAssistant {
			t.Fatalf("expected ActionAssistant, got %v", rec.ActionType)
		}
		if rec.Target == nil || rec.Target.Action != "open_chat" || rec.Target.Route != "/chat?stepId=step_bot" {
			t.Fatalf("unexpected target: %+v", rec.Target)
		}
	})

	t.Run("confirm action when step has no instructions but confirm allowed", func(t *testing.T) {
		step := Step{
			ID: "step_simple", Title: "Простой шаг", Status: "not_started",
			CompletionPolicy: "intern", AllowedActions: []string{"confirm"},
		}
		actionType, label, target := determineAction(step)
		if actionType != ActionConfirm || label != "Подтвердить выполнение" || target.Action != "open_card" {
			t.Fatalf("unexpected action: %s %s %+v", actionType, label, target)
		}
	})

	t.Run("independent availability filter branches", func(t *testing.T) {
		req := ports.AccessCheck{ResourceID: "repo:code", Permission: "write"}
		grants := map[ports.AccessCheck]bool{req: false}
		completed := map[string]bool{"prev": false}

		// 1. Completed step
		if isIndependentAvailable(Record{}, Step{Status: "completed"}, completed, grants) {
			t.Fatal("expected false for completed")
		}
		// 2. Blocked step
		if isIndependentAvailable(Record{}, Step{Status: "blocked"}, completed, grants) {
			t.Fatal("expected false for blocked")
		}
		// 3. Unmet prerequisites
		if isIndependentAvailable(Record{Prerequisites: []string{"prev"}}, Step{Status: "not_started"}, completed, grants) {
			t.Fatal("expected false for unmet prerequisites")
		}
		// 4. Missing grant
		if isIndependentAvailable(Record{Requirement: &req}, Step{Status: "not_started"}, completed, grants) {
			t.Fatal("expected false for missing grant")
		}
		// 5. Mentor completion policy
		if isIndependentAvailable(Record{}, Step{Status: "not_started", CompletionPolicy: "mentor"}, completed, grants) {
			t.Fatal("expected false for mentor policy")
		}
		// 6. Both completion policy already confirmed by intern
		bothStep := Step{
			Status:           "in_progress",
			CompletionPolicy: "both",
			Confirmations:    []Confirmation{{Role: identity.Intern, UserID: "intern_1"}},
		}
		if isIndependentAvailable(Record{}, bothStep, completed, grants) {
			t.Fatal("expected false for both already confirmed by intern")
		}
	})

	t.Run("custom blocked reasons and duration edge cases", func(t *testing.T) {
		customReason := "Специфическая причина"
		r := Record{
			Prerequisites: []string{"unmet"},
		}
		s := Step{
			Status:        "blocked",
			BlockedReason: &customReason,
		}
		isW, state, reason, _ := analyzeWaiting(r, s, map[string]bool{}, nil)
		if !isW || state != StateBlockedDeps || reason != customReason {
			t.Fatalf("expected customReason in deps: %v %s %s", isW, state, reason)
		}

		req := ports.AccessCheck{ResourceID: "r", Permission: "p"}
		r2 := Record{Requirement: &req}
		s2 := Step{Status: "blocked", BlockedReason: &customReason}
		isW2, state2, reason2, _ := analyzeWaiting(r2, s2, map[string]bool{}, map[ports.AccessCheck]bool{req: false})
		if !isW2 || state2 != StateWaitingAccess || reason2 != customReason {
			t.Fatalf("expected customReason in access: %v %s %s", isW2, state2, reason2)
		}

		s3 := Step{Status: "blocked", BlockedReason: &customReason}
		isW3, state3, reason3, _ := analyzeWaiting(Record{}, s3, map[string]bool{}, nil)
		if !isW3 || state3 != StateBlocked || reason3 != customReason {
			t.Fatalf("expected customReason in general: %v %s %s", isW3, state3, reason3)
		}

		// future timestamp (clock skew): dur < 0 -> 0
		future := time.Now().Add(10 * time.Minute)
		dur := computeDuration(&future)
		if dur == nil || *dur != 0 {
			t.Fatalf("expected 0 for future timestamp, got %v", dur)
		}
	})
}

func TestHttpRecommendationEndpoint(t *testing.T) {
	store := &memoryStore{
		snapshot: fixtureSnapshot(),
	}
	svc := Service{
		Store: store,
		Access: accessFunc(func(context.Context, ports.AccessCheck) (bool, error) {
			return true, nil
		}),
	}
	handler := Handler{Service: svc}

	// 1. Success case
	req := httptest.NewRequest("GET", "/api/v1/onboarding/recommendation", nil)
	req = req.WithContext(internContext())
	w := httptest.NewRecorder()
	handler.getRecommendation(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String())
	}
	var rec Recommendation
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil || rec.State == "" {
		t.Fatalf("failed to decode recommendation: %v", err)
	}

	// Also cover Handler.get
	wPlan := httptest.NewRecorder()
	handler.get(wPlan, req)
	if wPlan.Code != 200 {
		t.Fatalf("expected 200 for get plan, got %d %s", wPlan.Code, wPlan.Body.String())
	}

	// 2. Error case (unauthenticated)
	reqUnauth := httptest.NewRequest("GET", "/api/v1/onboarding/recommendation", nil)
	wUnauth := httptest.NewRecorder()
	handler.getRecommendation(wUnauth, reqUnauth)
	if wUnauth.Code == 200 {
		t.Fatalf("expected error code for unauthenticated context, got 200")
	}

	wPlanUnauth := httptest.NewRecorder()
	handler.get(wPlanUnauth, reqUnauth)
	if wPlanUnauth.Code == 200 {
		t.Fatalf("expected error code for get unauth, got 200")
	}
}
