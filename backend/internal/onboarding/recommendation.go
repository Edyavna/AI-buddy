package onboarding

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type RecommendationState string

const (
	StateActionAvailable RecommendationState = "action_available"
	StateWaitingMentor   RecommendationState = "waiting_mentor"
	StateWaitingAccess   RecommendationState = "waiting_access"
	StateBlockedDeps     RecommendationState = "blocked_dependencies"
	StateBlocked         RecommendationState = "blocked"
	StateCompleted       RecommendationState = "completed"
	StateEmptyPlan       RecommendationState = "empty_plan"
)

type ActionType string

const (
	ActionConfirm          ActionType = "confirm"
	ActionReadInstructions ActionType = "read_instructions"
	ActionAssistant        ActionType = "assistant"
	ActionViewStep         ActionType = "view_step"
)

type RecommendationTarget struct {
	StepID string `json:"step_id"`
	Route  string `json:"route"`
	Action string `json:"action"`
}

type WaitingStepInfo struct {
	StepID                 string              `json:"step_id"`
	Title                  string              `json:"title"`
	Period                 *string             `json:"period"`
	Reason                 string              `json:"reason"`
	State                  RecommendationState `json:"state"`
	AwaitingRole           *identity.Role      `json:"awaiting_role"`
	HelpDirection          *string             `json:"help_direction"`
	BlockedAt              *time.Time          `json:"blocked_at"`
	WaitingDurationSeconds *int64              `json:"waiting_duration_seconds"`
}

type RecommendationHelpAction struct {
	Direction string `json:"direction"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
}

type Recommendation struct {
	State       RecommendationState       `json:"state"`
	StepID      *string                   `json:"step_id"`
	Title       *string                   `json:"title"`
	Period      *string                   `json:"period"`
	Reason      string                    `json:"reason"`
	ActionType  *ActionType               `json:"action_type"`
	ActionLabel *string                   `json:"action_label"`
	Target      *RecommendationTarget     `json:"target"`
	WaitingStep *WaitingStepInfo          `json:"waiting_step"`
	HelpAction  *RecommendationHelpAction `json:"help_action"`
}

func computeRecommendation(snapshot Snapshot, steps []Step, grants map[ports.AccessCheck]bool, actor identity.Principal) (Recommendation, *string) {
	if len(steps) == 0 {
		return Recommendation{
			State:  StateEmptyPlan,
			Reason: "План адаптации пуст. Обратитесь к наставнику для назначения шагов.",
		}, nil
	}

	completed := snapshot.completedSteps()
	allCompleted := true
	for _, s := range steps {
		if s.Status != "completed" {
			allCompleted = false
			break
		}
	}
	if allCompleted {
		return Recommendation{
			State:  StateCompleted,
			Reason: "Все шаги плана успешно завершены!",
		}, nil
	}

	// Find the priority step: first non-completed step in plan order
	priorityIndex := -1
	for i, s := range steps {
		if s.Status != "completed" {
			priorityIndex = i
			break
		}
	}

	priorityStep := steps[priorityIndex]
	priorityRecord := snapshot.Steps[priorityIndex]

	isWaiting, waitingState, waitingReason, awaitingRole := analyzeWaiting(priorityRecord, priorityStep, completed, grants)

	if !isWaiting {
		// Priority step is directly actionable by intern!
		actionType, actionLabel, target := determineAction(priorityStep)
		nextID := priorityStep.ID
		return Recommendation{
			State:       StateActionAvailable,
			StepID:      &priorityStep.ID,
			Title:       &priorityStep.Title,
			Period:      priorityStep.Period,
			Reason:      "Текущий приоритетный шаг плана",
			ActionType:  &actionType,
			ActionLabel: &actionLabel,
			Target:      &target,
		}, &nextID
	}

	// Priority step is waiting or blocked!
	helpDir := determineHelpDirection(priorityStep)
	waitingDuration := computeDuration(priorityRecord.BlockedAt)

	waitingInfo := &WaitingStepInfo{
		StepID:                 priorityStep.ID,
		Title:                  priorityStep.Title,
		Period:                 priorityStep.Period,
		Reason:                 waitingReason,
		State:                  waitingState,
		AwaitingRole:           awaitingRole,
		HelpDirection:          &helpDir,
		BlockedAt:              priorityRecord.BlockedAt,
		WaitingDurationSeconds: waitingDuration,
	}

	// Search for the next independent available step
	for i := 0; i < len(steps); i++ {
		if i == priorityIndex {
			continue
		}
		s := steps[i]
		r := snapshot.Steps[i]
		if isIndependentAvailable(r, s, completed, grants) {
			actionType, actionLabel, target := determineAction(s)
			nextID := s.ID
			reason := fmt.Sprintf("Шаг не зависит от «%s» и готов к выполнению", priorityStep.Title)
			return Recommendation{
				State:       StateActionAvailable,
				StepID:      &s.ID,
				Title:       &s.Title,
				Period:      s.Period,
				Reason:      reason,
				ActionType:  &actionType,
				ActionLabel: &actionLabel,
				Target:      &target,
				WaitingStep: waitingInfo,
			}, &nextID
		}
	}

	// No independent available step found - entire remaining plan is waiting/blocked!
	helpAction := determineHelpAction(priorityStep, waitingState, helpDir, snapshot.HasHR)
	reason := fmt.Sprintf("Все доступные шаги ожидают действий: «%s» %s", priorityStep.Title, strings.ToLower(waitingReason))
	return Recommendation{
		State:       waitingState,
		Reason:      reason,
		WaitingStep: waitingInfo,
		HelpAction:  &helpAction,
	}, nil
}

func analyzeWaiting(r Record, s Step, completed map[string]bool, grants map[ports.AccessCheck]bool) (bool, RecommendationState, string, *identity.Role) {
	if s.Status == "blocked" {
		if !prerequisitesMet(r.Prerequisites, completed) {
			reason := "Сначала завершите предыдущий шаг"
			if s.BlockedReason != nil && *s.BlockedReason != "" {
				reason = *s.BlockedReason
			}
			return true, StateBlockedDeps, reason, nil
		}
		if r.Requirement != nil && !grants[*r.Requirement] {
			reason := "Ожидается получение доступа; подробности — в заявке и чате"
			if s.BlockedReason != nil && *s.BlockedReason != "" {
				reason = *s.BlockedReason
			}
			return true, StateWaitingAccess, reason, nil
		}
		reason := "Шаг заблокирован"
		if s.BlockedReason != nil && *s.BlockedReason != "" {
			reason = *s.BlockedReason
		}
		return true, StateBlocked, reason, nil
	}

	if s.CompletionPolicy == "mentor" {
		role := identity.Mentor
		return true, StateWaitingMentor, "Ожидает подтверждения наставника", &role
	}

	if s.CompletionPolicy == "both" && s.confirmedBy(identity.Intern) != "" {
		role := identity.Mentor
		return true, StateWaitingMentor, "Ожидает подтверждения наставника", &role
	}

	return false, "", "", nil
}

func isIndependentAvailable(r Record, s Step, completed map[string]bool, grants map[ports.AccessCheck]bool) bool {
	if s.Status == "completed" || s.Status == "blocked" {
		return false
	}
	if !prerequisitesMet(r.Prerequisites, completed) {
		return false
	}
	if r.Requirement != nil && !grants[*r.Requirement] {
		return false
	}
	if s.CompletionPolicy == "mentor" {
		return false
	}
	if s.CompletionPolicy == "both" && s.confirmedBy(identity.Intern) != "" {
		return false
	}
	return true
}

func determineAction(step Step) (ActionType, string, RecommendationTarget) {
	target := RecommendationTarget{
		StepID: step.ID,
		Route:  "/onboarding",
		Action: "open_card",
	}
	lowerInst := strings.ToLower(step.Instructions + " " + step.Description)
	if strings.Contains(lowerInst, "помощник") || strings.Contains(lowerInst, "чат") {
		target.Route = "/chat?stepId=" + step.ID
		target.Action = "open_chat"
		return ActionAssistant, "Спросить помощника", target
	}
	if len(step.Instructions) > 0 || len(step.Resources) > 0 {
		target.Action = "open_instructions"
		return ActionReadInstructions, "Читать инструкцию", target
	}
	if slices.Contains(step.AllowedActions, "confirm") {
		return ActionConfirm, "Подтвердить выполнение", target
	}
	return ActionViewStep, "Перейти к шагу", target
}

func determineHelpDirection(step Step) string {
	lower := strings.ToLower(step.TemplateKey + " " + step.Title + " " + step.Description + " " + step.Instructions)
	if strings.HasPrefix(step.TemplateKey, "hr_") || strings.Contains(lower, "кадр") || strings.Contains(lower, "отпуск") || strings.Contains(lower, "оформлен") || strings.Contains(lower, "трудов") {
		return "hr"
	}
	return "mentor"
}

func determineHelpAction(step Step, state RecommendationState, helpDir string, hasHR bool) RecommendationHelpAction {
	url := "/chat?stepId=" + step.ID
	if helpDir == "hr" {
		label := "Обратиться к HR"
		return RecommendationHelpAction{
			Direction: "hr",
			Label:     label,
			URL:       url,
			Available: hasHR,
		}
	}
	return RecommendationHelpAction{
		Direction: "mentor",
		Label:     "Задать вопрос наставнику",
		URL:       url,
		Available: true,
	}
}

func computeDuration(blockedAt *time.Time) *int64 {
	if blockedAt == nil {
		return nil
	}
	dur := int64(time.Since(*blockedAt).Seconds())
	if dur < 0 {
		dur = 0
	}
	return &dur
}
