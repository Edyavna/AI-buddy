package demo

import (
	"context"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// Agent is explicit synthetic output, with no invented documents or requests.
// Delay makes the stream visible and permits deterministic process-kill smoke tests.
type Agent struct{ Delay time.Duration }

func (a Agent) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	result := ports.AgentResult{Sources: []ports.Source{}, RequestIDs: []string{}}
	p, err := identity.Require(ctx)
	if err != nil {
		return result, err
	}
	if p.Role != identity.Intern {
		return result, ports.ErrForbidden
	}
	if err := emit(ctx, ports.AgentEvent{Kind: "tool.started", Tool: &ports.ToolEvent{CallID: "demo-context", Label: "Проверяю контекст шага"}}); err != nil {
		return result, err
	}
	if err := emit(ctx, ports.AgentEvent{Kind: "tool.finished", Tool: &ports.ToolEvent{CallID: "demo-context", Status: "succeeded"}}); err != nil {
		return result, err
	}
	for _, chunk := range []string{"Это тестовый ответ. ", "Вопрос сохранён ", "в истории диалога. ", "Для выбранного шага ", "можно уточнить инструкции ", "и необходимые доступы. ", "Настоящий агент ", "будет подключён отдельно."} {
		timer := time.NewTimer(a.Delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
		if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: chunk}); err != nil {
			return result, err
		}
	}
	return result, nil
}
