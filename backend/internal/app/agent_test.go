package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresAgentThroughHTTPProviders(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(agentProviderFixture))
	defer fixture.Close()
	cfg := config.Config{
		DemoMode: true, SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"},
		AI: &config.AIConfig{
			ModelBaseURL: fixture.URL, ModelName: "test-model", ModelKey: "test",
			EmbeddingBaseURL: fixture.URL, EmbeddingKey: "test",
		},
	}
	provider := integrations.New(db)
	deps := demoDependencies(db, provider, cfg.AI)
	k := deps.Knowledge.(*knowledge.Service)
	documents, err := knowledge.LoadMarkdown("../../../demo/knowledge")
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range documents {
		if err := k.Import(ctx, doc); err != nil {
			t.Fatal(doc.ID, err)
		}
	}

	h := NewHandler(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, deps)
	cookie := loginCookie(t, h, testkit.UserID, nil)
	w := apiRequest(t, h, "POST", "/api/v1/conversations", `{"client_conversation_id":"99999999-1111-4444-8888-000000000003","step_id":"step_local_setup"}`, cookie, 201)
	var conversation chat.Conversation
	if err := json.Unmarshal(w.Body.Bytes(), &conversation); err != nil || conversation.ID == "" {
		t.Fatal(err, conversation)
	}
	messagesURL := "/api/v1/conversations/" + conversation.ID + "/messages"
	r := httptest.NewRequest("POST", messagesURL, strings.NewReader(`{"client_message_id":"99999999-1111-4444-8888-000000000004","text":"Как запустить проект и получить доступ?"}`))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	stream := w.Body.String()
	draft, completed := strings.Index(stream, "event: action.proposed"), strings.Index(stream, "event: message.completed")
	if w.Code != 200 || draft < 0 || completed <= draft {
		t.Fatal(w.Code, stream)
	}
	w = apiRequest(t, h, "GET", messagesURL, "", cookie, 200)
	var messages []chat.Message
	if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil || len(messages) != 2 {
		t.Fatal(err, messages)
	}
	answer := messages[1]
	if len(answer.Sources) == 0 || len(answer.RequestIDs) != 1 {
		t.Fatalf("expected saved sources and draft: %+v", answer)
	}
	found := false
	for _, source := range answer.Sources {
		if source.DocumentID == "doc_other_team" {
			t.Fatal("foreign source leaked")
		}
		if source.DocumentID == "doc_local_setup" {
			found = true
		}
	}
	if !found {
		t.Fatal(answer.Sources)
	}
	apiRequest(t, h, "GET", "/api/v1/documents/doc_local_setup", "", cookie, 200)
	apiRequest(t, h, "GET", "/api/v1/documents/doc_other_team", "", cookie, 404)
	apiRequest(t, h, "GET", "/api/v1/documents/doc_local_setup", "", nil, 401)
	w = apiRequest(t, h, "GET", "/api/v1/access-requests/"+answer.RequestIDs[0], "", cookie, 200)
	var request httpapi.AccessRequest
	if err := json.Unmarshal(w.Body.Bytes(), &request); err != nil || request.Status != "draft" || request.ExternalID != nil {
		t.Fatal(err, request)
	}
	control, err := integrations.NewControl(db, true)
	if err != nil {
		t.Fatal(err)
	}
	assertExternalCount(t, control, 0)
	requestPath := "/api/v1/access-requests/" + request.ID
	apiRequest(t, h, "POST", requestPath+"/confirm", confirmBody, cookie, 202)
	worker := accessrequests.Worker{Store: accessrequests.Store{DB: db}, Provider: provider}
	runDue(t, ctx, worker)
	// Rebuild the handler to check recovery from persisted state.
	h = NewHandler(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, deps)
	w = apiRequest(t, h, "GET", requestPath, "", cookie, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &request); err != nil || request.Status != "pending" || request.ExternalID == nil {
		t.Fatal(err, request)
	}
	if err := control.Decide(ctx, request.ID, "granted", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE access_request_jobs SET next_attempt_at=now() WHERE request_id=$1", request.ID); err != nil {
		t.Fatal(err)
	}
	runDue(t, ctx, worker)
	plan := decodePlan(t, apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, cookie, 200))
	if plan.ProgressPercent != 5 || plan.Steps[0].Status != "completed" {
		t.Fatal(plan)
	}
	assertExternalCount(t, control, 1)
}

func agentProviderFixture(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/embeddings":
		var input struct {
			Task string `json:"task"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || (input.Task != "retrieval.passage" && input.Task != "retrieval.query") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		vector := make([]float64, 1024)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": vector}}})
	case "/chat/completions":
		var input struct {
			Model    string            `json:"model"`
			Messages []agent.Message   `json:"messages"`
			Format   map[string]string `json:"response_format"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.Model != "test-model" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if input.Format["type"] == "json_object" && len(input.Tools) == 0 {
			reply := agent.Message{Role: "assistant", Content: "{\"direction\":\"mentor\",\"reason\":\"Технический доступ\",\"clarification\":null,\"split\":false}"}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": reply, "finish_reason": "stop"}}})
			return
		}
		calls := []agent.Function{
			{Name: "get_my_tasks", Arguments: `{}`},
			{Name: "search_knowledge", Arguments: `{"query":"локальный запуск"}`},
			{Name: "read_document", Arguments: `{"document_id":"doc_local_setup"}`},
			{Name: "prepare_access_request", Arguments: `{"resource_id":"repo_demo_backend","permission":"read","reason":"Для запуска учебного проекта"}`},
		}
		round := 0
		for _, message := range input.Messages {
			if message.Role == "tool" {
				round++
			}
		}
		reply := agent.Message{Role: "assistant", Content: `{"answer":"Откройте инструкцию и подтвердите заявку.","outcome":"answered"}`}
		finish := "stop"
		if round < len(calls) {
			reply.Content = ""
			reply.Calls = []agent.Call{{ID: calls[round].Name, Type: "function", Function: calls[round]}}
			finish = "tool_calls"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": reply, "finish_reason": finish}}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
