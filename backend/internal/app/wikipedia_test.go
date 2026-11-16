package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresIssue84RoleAndWikipediaChat(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	var embeddingsUnavailable atomic.Bool
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embeddings":
			if embeddingsUnavailable.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			var in struct {
				Input []string
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			vector := make([]float64, 1024)
			vector[0] = 1
			data := make([]map[string]any, len(in.Input))
			for i := range data {
				data[i] = map[string]any{"index": i, "embedding": vector}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		case "/chat/completions":
			var in struct {
				Messages []agent.Message
				Tools    []json.RawMessage
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.Messages) < 2 {
				t.Error(in, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var reply string
			prompt, input := in.Messages[0].Content, in.Messages[1].Content
			switch {
			case strings.Contains(prompt, "Придумай короткое название"):
				reply = `{"title":"Вопрос об Авито"}`
			case strings.Contains(prompt, `direction="company"`):
				switch {
				case strings.Contains(input, "Когда основали"):
					reply = `{"direction":"company","reason":"История","clarification":null,"split":false}`
				case strings.Contains(input, "Когда ближайший набор"):
					reply = `{"direction":"hr","reason":"Набор","clarification":null,"split":false}`
				default:
					reply = `{"direction":"continue","reason":"Контекст","clarification":null,"split":false}`
				}
			case strings.Contains(prompt, "зафиксированной редакции Википедии"):
				if !strings.Contains(input, "Йонас") || !strings.Contains(input, "2007") || len(in.Tools) != 0 {
					t.Error("expected article passage and no tools")
				}
				reply = `{"answer":"Авито основали в 2007 году Йонас Нордландер и Филип Энгельберт."}`
			case len(in.Messages) > 2 && strings.Contains(in.Messages[2].Content, "Тестовый наставник") &&
				!strings.Contains(in.Messages[2].Content, "Второй наставник"):
				if !strings.Contains(prompt, "AI Buddy") || !strings.Contains(prompt, "прототип") {
					t.Error("role not set in system prompt")
				}
				reply = `{"answer":"Твой наставник — Тестовый наставник.","outcome":"answered"}`
			default:
				t.Error("unexpected provider call", prompt)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
				"finish_reason": "stop", "message": agent.Message{Content: reply},
			}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fixture.Close()
	cfg := config.AIConfig{ModelBaseURL: fixture.URL, ModelName: "fixture", ModelKey: "test", EmbeddingBaseURL: fixture.URL, EmbeddingKey: "test"}
	deps := demoDependencies(db, integrations.New(db), &cfg)
	if err := deps.Knowledge.(*knowledge.Service).ImportWikipedia(t.Context(), "../../../demo/wikipedia"); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, deps)
	intern := loginCookie(t, h, "intern_demo_1", nil)
	peer := loginCookie(t, h, "intern_demo_2", nil)
	conversation, _, answer := memoryChat(t, h, intern, 84001, "Когда основали Авито?")
	if !strings.Contains(answer, "2007 году") || strings.Contains(answer, "AvitoTech Playbook") {
		t.Fatal(answer)
	}
	items := hrResponse[[]chat.Message](t, h, "GET", "/conversations/"+conversation.ID+"/messages", nil, intern, 200)
	result := items[1]
	if result.Outcome != ports.OutcomeAnswered || result.Routing != nil || len(result.Sources) != 1 || result.Sources[0].Version != 155526969 {
		t.Fatal(result)
	}
	path := "/messages/" + result.ID + "/context-documents/" + result.Sources[0].DocumentID
	document := hrResponse[struct {
		OriginalURL string `json:"original_url"`
		Content     string `json:"content"`
	}](t, h, "GET", path, nil, intern, 200)
	if !strings.HasSuffix(document.OriginalURL, "&oldid=155526969") || !strings.Contains(document.Content, "CC BY-SA 4.0") {
		t.Fatal(document.OriginalURL)
	}
	hrResponse[any](t, h, "GET", path, nil, peer, 404)
	_, _, hiring := memoryChat(t, h, intern, 84003, "Когда ближайший набор стажёров?")
	if !strings.Contains(hiring, "недостаточно данных") || strings.Contains(hiring, "155526969") {
		t.Fatal(hiring)
	}
	_, _, mentor := memoryChat(t, h, intern, 84005, "Кто мой наставник?")
	if !strings.Contains(mentor, "Тестовый наставник") {
		t.Fatal(mentor)
	}
	embeddingsUnavailable.Store(true)
	_, _, unavailable := memoryChat(t, h, intern, 84007, "Когда основали Авито?")
	if !strings.Contains(unavailable, "сейчас недоступен") || strings.Contains(unavailable, "2007 году") {
		t.Fatal(unavailable)
	}
	for i, question := range []string{"Кто ты?", "Чем ты помогаешь?", "В какой компании я прохожу адаптацию?"} {
		_, _, role := memoryChat(t, h, intern, 84009+2*i, question)
		if !strings.Contains(role, "AI Buddy") || !strings.Contains(role, "прототип") {
			t.Fatal(role)
		}
	}
	demoHandler := requestHandler(db, Dependencies{})
	_, _, role := memoryChat(t, demoHandler, intern, 84020, "Кто ты?")
	if !strings.Contains(role, "AI Buddy") || !strings.Contains(role, "прототип") {
		t.Fatal(role)
	}
}
