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
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresMentorChatConfiguredModelAndIsolation(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var inputs [][]agent.Message
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []agent.Message
			Tools    json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if body.Model != "mentor-configured" || len(body.Tools) != 0 {
			t.Errorf("wrong model or intern tools: %+v", body)
		}
		mu.Lock()
		inputs = append(inputs, body.Messages)
		number := len(inputs)
		mu.Unlock()
		if number == 1 {
			w.WriteHeader(503)
			return
		}
		answer, _ := json.Marshal(map[string]string{"answer": "Личный ответ наставнику"})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": string(answer)}}}})
	}))
	defer modelServer.Close()
	model := agent.DeepSeek{Client: providerhttp.Client{BaseURL: modelServer.URL, Key: "fixture"}, Model: "mentor-configured"}
	deps := Dependencies{MentorAgent: agent.Mentor{Complete: model.CompleteWithoutTools}}
	h := requestHandler(db, deps)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	peer := loginCookie(t, h, "mentor_demo_2", nil)
	intern := loginCookie(t, h, "intern_demo_1", nil)
	hr := loginCookie(t, h, "hr_demo_1", nil)
	created := apiRequest(t, h, "POST", "/api/v1/conversations", `{"client_conversation_id":"`+hrKey(760)+`"}`, mentor, 201)
	var conversation chat.Conversation
	if err := json.Unmarshal(created.Body.Bytes(), &conversation); err != nil || conversation.ID == "" {
		t.Fatal(err, created.Body)
	}
	path := "/conversations/" + conversation.ID
	send := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest("POST", "/api/v1"+path+"/messages", strings.NewReader(body))
		request.Header.Set("Origin", "http://localhost:5173")
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(mentor)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") {
			t.Fatal(response.Code, response.Body)
		}
		return response
	}
	failed := send(`{"client_message_id":"` + hrKey(761) + `","text":"Личный вопрос наставника"}`)
	if !strings.Contains(failed.Body.String(), "LLM_UNAVAILABLE") {
		t.Fatal("missing durable model failure", failed.Body)
	}
	for index, text := range []string{"Личный вопрос наставника", "Продолжение личного разговора"} {
		body, _ := json.Marshal(map[string]string{"client_message_id": hrKey(762 + index), "text": text})
		reply := send(string(body))
		if !strings.Contains(reply.Body.String(), "message.completed") || strings.Contains(reply.Body.String(), "tool.started") || strings.Contains(reply.Body.String(), "action.proposed") {
			t.Fatal(reply.Body)
		}
	}
	mu.Lock()
	if len(inputs) != 3 || len(inputs[1]) != 2 || len(inputs[2]) != 4 || inputs[2][1].Content != "Личный вопрос наставника" || inputs[2][2].Content != "Личный ответ наставнику" {
		t.Errorf("model lost complete conversation pairs: %+v", inputs)
	}
	mu.Unlock()
	h = requestHandler(db, deps)
	messages := hrResponse[[]chat.Message](t, h, "GET", path+"/messages", nil, mentor, 200)
	if len(messages) != 6 || messages[1].Status != "failed" || messages[5].Text != "Личный ответ наставнику" {
		t.Fatal(messages)
	}
	for _, cookie := range []*http.Cookie{peer, intern} {
		for _, suffix := range []string{"", "/messages"} {
			hrResponse[any](t, h, "GET", path+suffix, nil, cookie, 404)
		}
		hrResponse[any](t, h, "PATCH", path, map[string]bool{"archived": true}, cookie, 404)
		apiRequest(t, h, "POST", "/api/v1"+path+"/messages", `{}`, cookie, 404)
	}
	hrResponse[any](t, h, "GET", "/conversations", nil, hr, 403)
	hrResponse[any](t, h, "POST", "/conversations", map[string]string{"client_conversation_id": hrKey(765), "step_id": "step_local_setup"}, mentor, 422)
	hrResponse[chat.Conversation](t, h, "PATCH", path, map[string]bool{"archived": true}, mentor, 200)
	if len(hrResponse[[]chat.Conversation](t, h, "GET", "/conversations", nil, mentor, 200)) != 0 {
		t.Fatal("archive filter ignored")
	}
	if len(hrResponse[[]chat.Conversation](t, h, "GET", "/conversations?archived=true", nil, mentor, 200)) != 1 {
		t.Fatal("archive lost")
	}
}
