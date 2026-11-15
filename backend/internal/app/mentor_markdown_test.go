package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresMentorMarkdownVersionsClarificationRetryAndArchive(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	versions := []string{"# Доступ\n\nИспользуйте репозиторий команды.\n", "# Доступ\n\nИспользуйте репозиторий команды.\n\n## Пример\n\nДля стажёра.\n", "# Доступ\n\n## Пример\n\nДля стажёра.\n\n## При отказе\n\nУточните у наставника.\n"}
	var mu sync.Mutex
	var inputs [][]agent.Message
	failed := false
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []agent.Message
			Tools    json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(body.Messages) < 2 || !strings.Contains(body.Messages[0].Content, "полный обновлённый документ") || len(body.Tools) != 0 {
			t.Error("wrong Markdown prompt or tools", body)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		inputs = append(inputs, body.Messages)
		text := body.Messages[len(body.Messages)-1].Content
		answer, clarify := versions[0], false
		switch text {
		case "Подготовь инструкцию":
			answer, clarify = "Какой репозиторий использует команда?", true
		case "Добавь пример":
			answer = versions[1]
		case "Добавь действия при отказе":
			if !failed {
				failed = true
				w.WriteHeader(503)
				return
			}
			answer = versions[2]
		}
		content, _ := json.Marshal(map[string]any{"answer": answer, "clarify": clarify})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": string(content)}}}})
	}))
	defer modelServer.Close()
	model := agent.DeepSeek{Client: providerhttp.Client{BaseURL: modelServer.URL, Key: "fixture"}, Model: "markdown-fixture"}
	deps := Dependencies{MentorAgent: agent.Mentor{Complete: model.CompleteWithoutTools}}
	h := requestHandler(db, deps)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	peer := loginCookie(t, h, "mentor_demo_2", nil)
	intern := loginCookie(t, h, "intern_demo_1", nil)
	hr := loginCookie(t, h, "hr_demo_1", nil)
	create := map[string]any{"client_conversation_id": hrKey(750), "markdown": true}
	hrResponse[any](t, h, "POST", "/conversations", create, mentor, 201)
	conversation := hrResponse[chat.Conversation](t, h, "POST", "/conversations", create, mentor, 200)
	if !conversation.Markdown || conversation.StepID != nil {
		t.Fatal(conversation)
	}
	hrResponse[any](t, h, "POST", "/conversations", map[string]any{"client_conversation_id": hrKey(750), "markdown": false}, mentor, 409)
	hrResponse[any](t, h, "POST", "/conversations", create, intern, 403)
	path := "/conversations/" + conversation.ID
	for i, text := range []string{"Подготовь инструкцию", "Репозиторий команды", "Добавь пример", "Добавь действия при отказе", "Добавь действия при отказе"} {
		body, _ := json.Marshal(map[string]string{"client_message_id": hrKey(751 + i), "text": text})
		r := httptest.NewRequest("POST", "/api/v1"+path+"/messages", strings.NewReader(string(body)))
		r.Header.Set("Origin", "http://localhost:5173")
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(mentor)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		terminal := "message.completed"
		if i == 3 {
			terminal = "message.failed"
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), terminal) || strings.Contains(w.Body.String(), "action.proposed") || strings.Contains(w.Body.String(), "tool.started") {
			t.Fatal(w.Code, w.Body)
		}
	}
	mu.Lock()
	if len(inputs) != 5 || len(inputs[2]) != 6 || inputs[2][4].Content != versions[0] || len(inputs[4]) != 8 || inputs[4][6].Content != versions[1] {
		t.Error("versions/clarifications lost or failed pair entered model history", inputs)
	}
	mu.Unlock()
	h = requestHandler(db, deps)
	history := hrResponse[[]chat.Message](t, h, "GET", path+"/messages", nil, mentor, 200)
	if len(history) != 10 || history[1].Outcome != ports.OutcomeNeedsClarification || history[3].Text != versions[0] || history[5].Text != versions[1] || history[7].Status != "failed" || history[9].Text != versions[2] {
		t.Fatal(history)
	}
	if !hrResponse[chat.Conversation](t, h, "GET", path, nil, mentor, 200).Markdown {
		t.Fatal("mode lost after reload")
	}
	hrResponse[chat.Conversation](t, h, "PATCH", path, map[string]bool{"archived": true}, mentor, 200)
	archived := hrResponse[[]chat.Conversation](t, h, "GET", "/conversations?archived=true", nil, mentor, 200)
	if len(archived) != 1 || !archived[0].Markdown || !archived[0].Archived {
		t.Fatal(archived)
	}
	for _, cookie := range []*http.Cookie{peer, intern} {
		for _, suffix := range []string{"", "/messages"} {
			hrResponse[any](t, h, "GET", path+suffix, nil, cookie, 404)
		}
		hrResponse[any](t, h, "PATCH", path, map[string]bool{"archived": false}, cookie, 404)
		apiRequest(t, h, "POST", "/api/v1"+path+"/messages", `{}`, cookie, 404)
	}
	hrResponse[any](t, h, "GET", path, nil, hr, 403)
	// Even the author loses Markdown access after losing the mentor role.
	if _, err := db.Exec(`UPDATE users SET role='intern' WHERE id='mentor_demo_1'`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, h, "GET", path, nil, mentor, 404)
	hrResponse[any](t, h, "GET", path+"/messages", nil, mentor, 404)
	hrResponse[any](t, h, "PATCH", path, map[string]bool{"archived": false}, mentor, 404)
	apiRequest(t, h, "POST", "/api/v1"+path+"/messages", `{}`, mentor, 404)
	if len(hrResponse[[]chat.Conversation](t, h, "GET", "/conversations?archived=true", nil, mentor, 200)) != 0 {
		t.Fatal("role-demoted author saw Markdown in archive")
	}
}
