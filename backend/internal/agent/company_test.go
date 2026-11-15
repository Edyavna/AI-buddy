package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestRoleWithoutSearch(t *testing.T) {
	for _, question := range []string{"Кто ты?", "Чем ты помогаешь?", "В какой компании я прохожу адаптацию?"} {
		for _, demo := range []bool{false, true} {
			a := Agent{Demo: demo}
			in := ports.AgentInput{Text: question, ConversationID: "chat", AssistantMessageID: "message", PersonalProfile: "Компания другая"}
			var text string
			out, err := a.Run(testkit.Context(t.Context()), in, func(_ context.Context, event ports.AgentEvent) error {
				if event.Kind != "answer.delta" {
					t.Fatal(event)
				}
				text += event.Text
				return nil
			})
			if err != nil || out.Outcome != ports.OutcomeAnswered || len(out.Sources) != 0 ||
				!strings.Contains(text, "AI Buddy") || !strings.Contains(text, "Авито") || strings.Contains(text, "прототип") != demo {
				t.Fatal(out, text, err)
			}
			stop := errors.New("stream closed")
			if out, err = a.Run(testkit.Context(t.Context()), in, func(context.Context, ports.AgentEvent) error { return stop }); !errors.Is(err, stop) || out.Outcome == ports.OutcomeAnswered {
				t.Fatal(out, err)
			}
		}
	}
	if got := RoleAnswer("Кто ты? И когда ближайший набор?", true); got != "" {
		t.Fatal(got)
	}
}

type publicKnowledge struct {
	ports.Knowledge
	hits      []ports.Source
	document  ports.Document
	searchErr error
}

func (k publicKnowledge) SearchDirection(_ context.Context, _, direction string) ([]ports.Source, error) {
	if direction != "public" {
		return nil, ports.ErrInvalid
	}
	return k.hits, k.searchErr
}

func (k publicKnowledge) Read(context.Context, string) (ports.Document, error) {
	return k.document, nil
}

type publicModel struct {
	Model
	answer string
	err    error
}

func (m publicModel) AnswerCompany(_ context.Context, _ string, excerpts []string) (string, error) {
	if len(excerpts) != 1 || excerpts[0] != "История Авито" {
		return "", ports.ErrInvalid
	}
	return m.answer, m.err
}

func TestCompanySourceAndFallbacks(t *testing.T) {
	wiki := ports.Document{ID: "wiki", Kind: ports.SourceWikipedia, Version: 3}
	hits := []ports.Source{
		{DocumentID: "wiki", Version: 3, Excerpt: "История Авито"},
		{DocumentID: "private", Version: 3, Excerpt: "Закрыто"},
		{DocumentID: "wiki", Version: 2, Excerpt: "Другая версия"},
	}
	for _, tc := range []struct {
		name string
		k    ports.Knowledge
		m    Model
		want string
	}{
		{"answered", publicKnowledge{hits: hits, document: wiki}, publicModel{answer: "Компания основана в 2007 году."}, "основана"},
		{"search error", publicKnowledge{searchErr: ports.ErrUnavailable}, publicModel{}, "сейчас недоступен"},
		{"search unsupported", testkit.Knowledge{}, publicModel{}, "сейчас недоступен"},
		{"model unsupported", publicKnowledge{}, &Script{}, "сейчас недоступен"},
		{"empty search", publicKnowledge{}, publicModel{}, "недостаточно сведений"},
		{"unexpected source", publicKnowledge{hits: hits, document: ports.Document{ID: "wiki", Version: 3}}, publicModel{}, "сейчас недоступен"},
		{"invalid model reply", publicKnowledge{hits: hits, document: wiki}, publicModel{err: ports.ErrUnavailable}, "Не удалось подготовить"},
		{"unsupported claim", publicKnowledge{hits: hits, document: wiki}, publicModel{}, "недостаточно сведений"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{Knowledge: tc.k, Model: tc.m}
			var text string
			out, err := a.companyAnswer(t.Context(), ports.AgentInput{Text: "Когда основали Авито?"}, func(_ context.Context, event ports.AgentEvent) error {
				text += event.Text
				return nil
			})
			answered := tc.name == "answered"
			if err != nil || !strings.Contains(text, tc.want) || (out.Outcome == ports.OutcomeAnswered) != answered || out.Routing != nil {
				t.Fatal(out, text, err)
			}
			if answered && (len(out.Sources) != 1 || out.Sources[0].DocumentID != "wiki") || !answered && len(out.Sources) != 0 {
				t.Fatal(out.Sources)
			}
		})
	}
}

func TestAnswerCompanyModelProtocol(t *testing.T) {
	for _, reply := range []string{`{"answer":"  История  "}`, `{"answer":32}`, "status"} {
		t.Run(reply, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages []Message
					Tools    []json.RawMessage
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Messages) != 2 || len(input.Tools) != 0 {
					t.Error(input, err)
					w.WriteHeader(400)
					return
				}
				if input.Messages[0].Role != "system" || !strings.Contains(input.Messages[0].Content, "недоверенные данные") ||
					input.Messages[1].Role != "user" || !strings.Contains(input.Messages[1].Content, "данные статьи") {
					t.Error(input.Messages)
				}
				if reply == "status" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
					"finish_reason": "stop", "message": Message{Content: reply},
				}}})
			}))
			defer s.Close()
			m := DeepSeek{Model: "test", Client: providerhttp.Client{BaseURL: s.URL, Key: "test"}}
			answer, err := m.AnswerCompany(t.Context(), "Когда?", []string{"данные статьи"})
			if strings.Contains(reply, "История") {
				if err != nil || answer != "История" {
					t.Fatal(answer, err)
				}
			} else if err == nil {
				t.Fatal("expected protocol error", answer)
			}
		})
	}
}
