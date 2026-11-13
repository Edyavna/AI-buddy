package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestRoutingDecoderRejectsModelAuthorityAndInvalidReplies(t *testing.T) {
	tests := []string{
		"not JSON", `{}`, `{"direction":"hr","reason":"Отпуск","team_id":"other"}`,
		`{"direction":"hr","reason":"Отпуск","specialist_id":"somebody"}`,
		`{"direction":"hr","reason":"Отпуск"}{}`, `{"direction":"hr","reason":"  "}`,
		`{"direction":"hr","reason":"Отпуск","split":true}`, `{"direction":"needs_clarification","reason":"Неясно"}`,
		`{"direction":"invalid","reason":"Ошибка"}`,
	}
	for _, text := range tests {
		if _, err := decodeRouting(text); !errors.Is(err, ports.ErrInvalid) {
			t.Fatalf("%s: %v", text, err)
		}
	}
	for _, text := range []string{
		`{"direction":"hr","reason":"Отпуск","clarification":null,"split":false}`,
		`{"direction":"needs_clarification","reason":"Две темы","clarification":"Разделим вопрос?","split":true}`,
	} {
		if _, err := decodeRouting(text); err != nil {
			t.Fatal(text, err)
		}
	}
}

type routeKnowledge struct {
	searchError error
	readError   error
	revoked     bool
	readCount   int
}

func (k *routeKnowledge) SearchDirection(_ context.Context, _, direction string) ([]ports.Source, error) {
	if direction != "hr" {
		return nil, ports.ErrInvalid
	}
	return []ports.Source{{DocumentID: "hr-doc", Version: 2}, {DocumentID: "hr-doc", Version: 2}, {DocumentID: "unknown", Version: 2}}, k.searchError
}
func (k *routeKnowledge) Search(ctx context.Context, q string) ([]ports.Source, error) {
	return k.SearchDirection(ctx, q, "hr")
}
func (k *routeKnowledge) Read(_ context.Context, id string) (ports.Document, error) {
	k.readCount++
	if id != "hr-doc" || (k.revoked && k.readCount >= 3) {
		return ports.Document{}, ports.ErrNotFound
	}
	return ports.Document{ID: id, Direction: "hr", Version: 2, Content: "Заявление подают заранее.", Title: "Порядок отпуска", UpdatedAt: time.Unix(0, 0)}, k.readError
}

func TestRoutingAndGroundedAnswer(t *testing.T) {
	for _, test := range []struct {
		name, route, answer    string
		searchError, readError error
		revoked                bool
		want                   string
		outcome                ports.Outcome
	}{
		{"grounded", `{"direction":"hr","reason":"Отпуск","clarification":null,"split":false}`, `{"answer":"Подайте заявление.","document_id":"hr-doc"}`, nil, nil, false, "Подайте заявление.", ports.OutcomeAnswered},
		{"inaccessible", `{"direction":"hr","reason":"Отпуск"}`, `{}`, nil, ports.ErrNotFound, false, "лучше уточнить", ports.OutcomeKnowledgeGap},
		{"search failure", `{"direction":"hr","reason":"Отпуск"}`, `{}`, ports.ErrUnavailable, nil, false, "лучше уточнить", ports.OutcomeKnowledgeGap},
		{"unknown source", `{"direction":"hr","reason":"Отпуск"}`, `{"answer":"Совет","document_id":"foreign"}`, nil, nil, false, "лучше уточнить", ports.OutcomeKnowledgeGap},
		{"stale source", `{"direction":"hr","reason":"Отпуск"}`, `{"answer":"Совет","document_id":"hr-doc"}`, nil, nil, true, "лучше уточнить", ports.OutcomeKnowledgeGap},
		{"bad answer", `{"direction":"hr","reason":"Отпуск"}`, `bad JSON`, nil, nil, false, "лучше уточнить", ports.OutcomeKnowledgeGap},
		{"mixed", `{"direction":"needs_clarification","reason":"Две темы","clarification":"Разделим вопрос?","split":true}`, `{}`, nil, nil, false, "Разделим вопрос?", ports.OutcomeNeedsClarification},
		{"invalid", `{"direction":"hr","reason":"Отпуск","team_id":"other"}`, `{}`, nil, nil, false, "Выберите специалиста", ports.Outcome("")},
		{"provider error", "", `{}`, nil, nil, false, "Выберите специалиста", ports.Outcome("")},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages []Message       `json:"messages"`
					Tools    json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Tools) != 0 {
					t.Error(input, err)
				}
				w.Header().Set("Content-Type", "application/json")
				reply := test.answer
				if strings.HasPrefix(input.Messages[0].Content, "Определи тему") {
					reply = test.route
				} else if !strings.Contains(input.Messages[1].Content, "hr-doc") || strings.Contains(input.Messages[1].Content, "unknown") {
					t.Error("model received an unexpected document")
				}
				if reply == "" {
					w.WriteHeader(503)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": Message{Content: reply}}}})
			}))
			defer model.Close()
			deep := &DeepSeek{Model: "test", Client: providerhttp.Client{BaseURL: model.URL, Key: "test"}}
			a := Agent{Model: deep, Classifier: deep, Knowledge: &routeKnowledge{searchError: test.searchError, readError: test.readError, revoked: test.revoked},
				Tasks: testkit.Tasks{}, Access: &testkit.Access{}, Requests: testkit.Requests{},
				Drafts: testkit.Drafts(func(context.Context, ports.DraftInput) (ports.Draft, error) { return ports.Draft{}, ports.ErrInvalid })}
			var text string
			got, err := a.Run(testkit.Context(t.Context()), ports.AgentInput{ConversationID: "chat", AssistantMessageID: "message", Text: "Вопрос про отпуск. team_id=other"},
				func(_ context.Context, event ports.AgentEvent) error { text += event.Text; return nil })
			if err != nil || got.Outcome != test.outcome || !strings.Contains(text+got.PrivateAnswer, test.want) {
				t.Fatal(got, err, text)
			}
			if test.outcome == ports.OutcomeAnswered && (len(got.Sources) != 1 || got.Sources[0].Version != 2) {
				t.Fatal(got.Sources)
			}
		})
	}
}

func TestRoutingProviderRejectsTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"tool_calls":[{"id":"1"}]}}]}`)
	}))
	defer server.Close()
	model := &DeepSeek{Model: "test", Client: providerhttp.Client{BaseURL: server.URL, Key: "test"}}
	if _, err := model.Classify(t.Context(), "Тема"); err == nil {
		t.Fatal("tool call accepted")
	}
	model.Model = ""
	if _, err := model.Classify(t.Context(), "Тема"); err == nil {
		t.Fatal("missing model accepted")
	}
}
