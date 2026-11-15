package onboarding

import (
	"net/url"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func TestRecommendationRechecksAccessForPriorityStep(t *testing.T) {
	requirement := ports.AccessCheck{ResourceID: "repo", Permission: "read"}
	snapshot := Snapshot{Steps: []Record{
		{Step: Step{ID: "first", Title: "Работа с репозиторием", Status: "in_progress", CompletionPolicy: "intern", Instructions: "Инструкция"}, Requirement: &requirement},
		{Step: Step{ID: "independent", Status: "not_started", CompletionPolicy: "intern", Instructions: "Прочитать"}},
	}}
	plan := project(snapshot, map[ports.AccessCheck]bool{requirement: false}, identity.Principal{UserID: "intern", Role: identity.Intern})
	if plan.NextStepID == nil || *plan.NextStepID != "independent" || plan.Recommendation.WaitingStep == nil || plan.Recommendation.WaitingStep.State != StateWaitingAccess {
		t.Fatalf("unavailable priority step was recommended: %+v", plan.Recommendation)
	}
}

func TestRecommendationChatURLsPreserveObjectAndRecipient(t *testing.T) {
	step := Step{ID: "step &?#+/", TemplateKey: "hr_vacation", Title: "Кадровые документы", Instructions: "Спросить помощника"}
	action := determineHelpAction(step, StateBlocked, "hr", true)
	route, err := url.Parse(action.URL)
	if err != nil || route.Path != "/chat" || route.Query().Get("stepId") != step.ID || route.Query().Get("direction") != "hr" {
		t.Fatalf("lost source or recipient: %q %v", action.URL, err)
	}
	_, _, target := determineAction(step)
	route, err = url.Parse(target.Route)
	if err != nil || route.Query().Get("stepId") != step.ID {
		t.Fatalf("lost chat context: %q %v", target.Route, err)
	}
}

func TestRecommendationMentorWaitStartsAtInternConfirmation(t *testing.T) {
	confirmed := time.Now().UTC().Add(-10 * time.Minute)
	snapshot := Snapshot{Steps: []Record{{Step: Step{ID: "both", Status: "in_progress", CompletionPolicy: "both", Confirmations: []Confirmation{{Role: identity.Intern, UserID: "intern", ConfirmedAt: confirmed}}}}}}
	plan := project(snapshot, nil, identity.Principal{UserID: "intern", Role: identity.Intern})
	wait := plan.Recommendation.WaitingStep
	if wait == nil || wait.BlockedAt == nil || !wait.BlockedAt.Equal(confirmed) || wait.WaitingDurationSeconds == nil || *wait.WaitingDurationSeconds < 599 {
		t.Fatalf("lost persisted confirmation timestamp: %+v", wait)
	}
}

func TestRecommendationDoesNotMistakeStartForChat(t *testing.T) {
	kind, _, _ := determineAction(Step{ID: "start", Instructions: "Начать работу: прочитайте инструкцию"})
	if kind != ActionReadInstructions {
		t.Fatalf("instruction incorrectly opens chat: %s", kind)
	}
}
