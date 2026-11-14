package demo

import (
	"context"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type MentorAgent struct{}

func (MentorAgent) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	answer := "Демонстрационный ответ наставнику: " + in.Text
	if in.Markdown {
		answer = "# Демонстрационная инструкция\n\n" + in.Text
	}
	if len(in.History) >= 2 {
		answer += "\n\nПредыдущий вопрос: " + in.History[len(in.History)-2].Text
	}
	if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: answer}); err != nil {
		return ports.AgentResult{}, err
	}
	return ports.AgentResult{Outcome: ports.OutcomeAnswered}, nil
}
