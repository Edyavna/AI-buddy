package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func TestMentorModel(t *testing.T) {
	for _, tc := range []struct {
		name, content     string
		markdown          bool
		modelErr, emitErr error
		calls             []Call
		outcome           ports.Outcome
		wantErr           error
	}{
		{name: "chat", content: `{"answer":"Продолжение"}`, outcome: ports.OutcomeAnswered},
		{name: "document", markdown: true, content: `{"answer":"# Инструкция","clarify":false}`, outcome: ports.OutcomeAnswered},
		{name: "clarification", markdown: true, content: `{"answer":"Какой репозиторий?","clarify":true}`, outcome: ports.OutcomeNeedsClarification},
		{name: "missing decision", markdown: true, content: `{"answer":"# Инструкция"}`, wantErr: ports.ErrUnavailable},
		{name: "invalid", content: "invalid", wantErr: ports.ErrUnavailable},
		{name: "blank", content: `{"answer":" "}`, wantErr: ports.ErrUnavailable},
		{name: "tool", calls: []Call{{ID: "tool"}}, wantErr: ports.ErrUnavailable},
		{name: "provider", modelErr: ports.ErrUnavailable, wantErr: ports.ErrUnavailable},
		{name: "cancelled", content: `{"answer":"Ответ"}`, emitErr: context.Canceled, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := Mentor{Complete: func(_ context.Context, messages []Message) (Message, error) {
				if len(messages) != 4 || strings.Contains(messages[0].Content, "clarify") != tc.markdown ||
					messages[1].Content != "Первый вопрос" || messages[2].Content != "# Предыдущий ответ" || messages[3].Content != "Обнови" {
					t.Fatalf("unexpected model input: %+v", messages)
				}
				return Message{Content: tc.content, Calls: tc.calls}, tc.modelErr
			}}
			var answer string
			got, err := model.Run(t.Context(), ports.AgentInput{Text: "Обнови", Markdown: tc.markdown, History: []ports.ChatMessage{
				{Role: "user", Text: "Первый вопрос"}, {Role: "assistant", Text: "# Предыдущий ответ"},
			}}, func(_ context.Context, event ports.AgentEvent) error {
				if event.Kind != "answer.delta" {
					t.Fatal(event)
				}
				answer = event.Text
				return tc.emitErr
			})
			if !errors.Is(err, tc.wantErr) || got.Outcome != tc.outcome || (err == nil && answer == "") {
				t.Fatalf("%+v, %v, %q", got, err, answer)
			}
		})
	}
}
