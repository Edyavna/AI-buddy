package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Mentor struct {
	Complete func(context.Context, []Message) (Message, error)
}

func (a Mentor) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	if a.Complete == nil {
		return ports.AgentResult{}, ports.ErrUnavailable
	}
	const prompt = `Ты помощник наставника. Отвечай на обычные вопросы, учитывая историю. Если рабочих фактов недостаточно, попроси их уточнить. Верни один объект JSON с полем "answer" (текст в Markdown).`
	messages := []Message{{Role: "system", Content: prompt}}
	for _, previous := range in.History {
		messages = append(messages, Message{Role: previous.Role, Content: previous.Text})
	}
	messages = append(messages, Message{Role: "user", Content: in.Text})
	reply, err := a.Complete(ctx, messages)
	if err != nil {
		return ports.AgentResult{}, err
	}
	var result struct {
		Answer string `json:"answer"`
	}
	if len(reply.Calls) != 0 || json.Unmarshal([]byte(reply.Content), &result) != nil || strings.TrimSpace(result.Answer) == "" {
		return ports.AgentResult{}, ports.ErrUnavailable
	}
	if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: result.Answer}); err != nil {
		return ports.AgentResult{}, err
	}
	return ports.AgentResult{Outcome: ports.OutcomeAnswered}, nil
}
