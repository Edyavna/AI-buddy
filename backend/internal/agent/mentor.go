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
	const prompt = `Ты помощник наставника. Отвечай на обычные вопросы, учитывая историю. Если рабочих фактов недостаточно, попроси их уточнить. Верни один объект JSON с полем "answer" (текст в Markdown).`
	instructions := prompt
	if in.Markdown {
		instructions = `Ты помогаешь наставнику подготовить инструкцию для базы знаний. Верни один JSON-объект с полями "answer" и "clarify" (булево). Если не хватает рабочих фактов, задай конкретный вопрос в answer и установи clarify=true. Иначе установи clarify=false и помести полный Markdown-документ в answer без внешнего блока кода. Для правок учитывай последнюю версию и предыдущие уточнения, всегда возвращай полный обновлённый документ. Не придумывай внутренние правила, контакты, сроки или ссылки.`
	}
	messages := []Message{{Role: "system", Content: instructions}}
	for _, previous := range in.History {
		messages = append(messages, Message{Role: previous.Role, Content: previous.Text})
	}
	messages = append(messages, Message{Role: "user", Content: in.Text})
	reply, err := a.Complete(ctx, messages)
	if err != nil {
		return ports.AgentResult{}, err
	}
	var result struct {
		Answer  string `json:"answer"`
		Clarify *bool  `json:"clarify"`
	}
	if len(reply.Calls) != 0 || json.Unmarshal([]byte(reply.Content), &result) != nil || strings.TrimSpace(result.Answer) == "" || (in.Markdown && result.Clarify == nil) {
		return ports.AgentResult{}, ports.ErrUnavailable
	}
	if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: result.Answer}); err != nil {
		return ports.AgentResult{}, err
	}
	if in.Markdown && *result.Clarify {
		return ports.AgentResult{Outcome: ports.OutcomeNeedsClarification}, nil
	}
	return ports.AgentResult{Outcome: ports.OutcomeAnswered}, nil
}
