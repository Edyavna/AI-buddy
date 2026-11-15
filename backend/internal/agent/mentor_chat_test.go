package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
)

func TestMentorChatUsesModelHistoryWithoutTools(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Model    string
			Messages []Message
			Tools    json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if request.Model != "configured" || len(request.Tools) != 0 || len(request.Messages) != 4 ||
			request.Messages[1].Content != "Первый вопрос" || request.Messages[2].Content != "Первый ответ" ||
			request.Messages[3].Content != "Уточнение" {
			t.Errorf("unexpected model request: %+v", request)
		}
		switch calls {
		case 1:
			w.WriteHeader(503)
		case 2:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"answer\":\"Продолжение\"}"},"finish_reason":"stop"}]}`))
		}
	}))
	defer server.Close()
	model := DeepSeek{Client: providerhttp.Client{BaseURL: server.URL, Key: "fixture"}, Model: "configured"}
	agent := Mentor{Complete: model.CompleteWithoutTools}
	input := ports.AgentInput{Text: "Уточнение", History: []ports.ChatMessage{
		{Role: "user", Text: "Первый вопрос"}, {Role: "assistant", Text: "Первый ответ"},
	}}
	var emitErr error
	var answer string
	emit := func(_ context.Context, event ports.AgentEvent) error {
		answer = event.Text
		return emitErr
	}
	for i := 0; i < 2; i++ {
		if _, err := agent.Run(t.Context(), input, emit); !errors.Is(err, ports.ErrUnavailable) || answer != "" {
			t.Fatalf("unexpected failure result: %q, %v", answer, err)
		}
	}
	emitErr = context.Canceled
	if _, err := agent.Run(t.Context(), input, emit); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	emitErr = nil
	if result, err := agent.Run(t.Context(), input, emit); err != nil || result.Outcome != ports.OutcomeAnswered || answer != "Продолжение" {
		t.Fatalf("%+v, %q, %v", result, answer, err)
	}
}

func TestMentorChatMissingModelIsUnavailable(t *testing.T) {
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("unconfigured mentor model panicked: %v", value)
		}
	}()
	_, err := (Mentor{}).Run(t.Context(), ports.AgentInput{Text: "Вопрос"}, func(context.Context, ports.AgentEvent) error { t.Fatal("unexpected answer"); return nil })
	if !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
}
