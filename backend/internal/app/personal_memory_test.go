package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/personalmemory"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func memoryChat(t *testing.T, h http.Handler, cookie *http.Cookie, n int, text string) (chat.Conversation, chat.Message, string) {
	t.Helper()
	var c chat.Conversation
	created := apiRequest(t, h, "POST", "/api/v1/conversations", `{"client_conversation_id":"`+hrKey(n)+`"}`, cookie, 201)
	if e := json.Unmarshal(created.Body.Bytes(), &c); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]string{"client_message_id": hrKey(n + 1), "text": text})
	request := httptest.NewRequest("POST", "/api/v1/conversations/"+c.ID+"/messages", strings.NewReader(string(raw)))
	request.AddCookie(cookie)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "message.completed") {
		t.Fatal(rec.Body.String())
	}
	items := hrResponse[[]chat.Message](t, h, "GET", "/conversations/"+c.ID+"/messages", nil, cookie, 200)
	if len(items) != 2 {
		t.Fatal(items)
	}
	return c, items[0], rec.Body.String()
}
func TestPostgresPersonalMemoryCompleteAPIAndFreshDialogs(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	intern := loginCookie(t, h, "intern_demo_1", nil)
	peer := loginCookie(t, h, "intern_demo_2", nil)
	foreign := loginCookie(t, h, "intern_demo_other", nil)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	hr := loginCookie(t, h, "hr_demo_1", nil)
	_, message, _ := memoryChat(t, h, intern, 79001, "Я работаю на Windows и использую PowerShell.")
	path := "/messages/" + message.ID + "/memory-proposals"
	initial := hrResponse[personalmemory.Proposals](t, h, "GET", path, nil, intern, 200)
	if initial.State != "not_requested" {
		t.Fatal(initial)
	}
	v := hrResponse[personalmemory.Proposals](t, h, "POST", path, map[string]any{}, intern, 200)
	if len(v.Items) != 2 {
		t.Fatal(v)
	}
	workspace := hrResponse[personalmemory.Workspace](t, h, "GET", "/memory", nil, intern, 200)
	if len(workspace.Entries) != 0 {
		t.Fatal("automatic write", workspace)
	}
	var proposal personalmemory.Proposal
	for _, p := range v.Items {
		if p.Key == "shell" {
			proposal = p
		}
	}
	for _, cookie := range []*http.Cookie{peer, foreign} {
		hrResponse[any](t, h, "GET", path, nil, cookie, 404)
		hrResponse[any](t, h, "POST", path, map[string]any{}, cookie, 404)
		hrResponse[any](t, h, "PUT", "/memory/proposals/"+proposal.ID, personalmemory.Decision{Decision: "accept"}, cookie, 404)
	}
	result := hrResponse[personalmemory.DecisionResult](t, h, "PUT", "/memory/proposals/"+proposal.ID, personalmemory.Decision{Decision: "accept"}, intern, 200)
	repeated := hrResponse[personalmemory.DecisionResult](t, h, "PUT", "/memory/proposals/"+proposal.ID, personalmemory.Decision{Decision: "accept"}, intern, 200)
	if result.Entry == nil || repeated.Entry.ID != result.Entry.ID {
		t.Fatal(result, repeated)
	}
	hrResponse[any](t, h, "PUT", "/memory/entries/"+result.Entry.ID, map[string]any{"value": "password_secret", "revision": 1}, intern, 422)
	for _, cookie := range []*http.Cookie{peer, foreign} {
		hrResponse[any](t, h, "GET", "/memory/entries/"+result.Entry.ID, nil, cookie, 404)
		hrResponse[any](t, h, "PUT", "/memory/entries/"+result.Entry.ID, map[string]any{"value": "bash", "revision": 1}, cookie, 404)
		hrResponse[any](t, h, "DELETE", "/memory/entries/"+result.Entry.ID, map[string]int{"revision": 1}, cookie, 404)
		w := hrResponse[personalmemory.Workspace](t, h, "GET", "/memory", nil, cookie, 200)
		if len(w.Entries) != 0 || strings.Contains(w.Markdown, "PowerShell") {
			t.Fatal(w)
		}
	}
	for _, cookie := range []*http.Cookie{mentor, hr} {
		hrResponse[any](t, h, "GET", "/memory", nil, cookie, 403)
		hrResponse[any](t, h, "GET", "/memory/entries/"+result.Entry.ID, nil, cookie, 403)
	}
	_, _, answer := memoryChat(t, h, intern, 79003, "Как проверить текущую папку?")
	if !strings.Contains(answer, "Get-Location") {
		t.Fatal(answer)
	}
	_, _, answer = memoryChat(t, h, peer, 79005, "Как проверить текущую папку?")
	if strings.Contains(answer, "Get-Location") {
		t.Fatal("another user's preference reached model", answer)
	}
	updated := hrResponse[personalmemory.Entry](t, h, "PUT", "/memory/entries/"+result.Entry.ID, map[string]any{"value": "bash", "revision": 1}, intern, 200)
	_, _, answer = memoryChat(t, h, intern, 79007, "Как проверить текущую папку?")
	if !strings.Contains(answer, "pwd") || strings.Contains(answer, "Get-Location") {
		t.Fatal(answer)
	}
	hrResponse[any](t, h, "PUT", "/memory/entries/"+result.Entry.ID, map[string]any{"value": "powershell", "revision": 1}, intern, 409)
	if _, err := db.Exec(`UPDATE mentor_assignments SET mentor_id='mentor_demo_2' WHERE intern_id='intern_demo_1'`); err != nil {
		t.Fatal(err)
	}
	workspace = hrResponse[personalmemory.Workspace](t, h, "GET", "/memory", nil, intern, 200)
	if workspace.Context.Mentor.ID != "mentor_demo_2" {
		t.Fatal(workspace)
	}
	seen := make(chan string, 1)
	h = requestHandler(db, Dependencies{Agent: memoryProfileProbe{seen}})
	memoryChat(t, h, intern, 79011, "Кто мой наставник?")
	profile := <-seen
	if !strings.Contains(profile, workspace.Context.Mentor.Name) || strings.Contains(profile, "Тестовый наставник") || !strings.Contains(profile, "Использую Bash") {
		t.Fatal(profile)
	}
	// A new handler simulates process reconstruction; no in-memory preference store.
	h = requestHandler(db, Dependencies{})
	workspace = hrResponse[personalmemory.Workspace](t, h, "GET", "/memory", nil, intern, 200)
	if len(workspace.Entries) != 1 || workspace.Entries[0].Value != "bash" {
		t.Fatal(workspace)
	}
	hrResponse[any](t, h, "DELETE", "/memory/entries/"+result.Entry.ID, map[string]int64{"revision": updated.Revision}, intern, 200)
	hrResponse[any](t, h, "DELETE", "/memory/entries/"+result.Entry.ID, map[string]int64{"revision": updated.Revision}, intern, 200)
	receipt := apiRequest(t, h, "GET", "/api/v1/memory/entries/"+result.Entry.ID, "", intern, 404)
	if !strings.Contains(receipt.Body.String(), "MEMORY_ENTRY_DELETED") {
		t.Fatal(receipt.Body.String())
	}
	_, _, answer = memoryChat(t, h, intern, 79009, "Как проверить текущую папку?")
	if strings.Contains(answer, "Get-Location") || strings.Contains(answer, "pwd") {
		t.Fatal(answer)
	}
	hrResponse[any](t, h, "GET", "/memory", nil, nil, 401)
}

type failedMemorySelector struct{}

func (failedMemorySelector) Select(context.Context, string) ([]personalmemory.Candidate, error) {
	return nil, ports.ErrUnavailable
}
func TestPostgresPersonalMemoryMalformedRequestsAndModelFailure(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{MemorySelector: failedMemorySelector{}})
	intern := loginCookie(t, h, "intern_demo_1", nil)
	_, message, _ := memoryChat(t, h, intern, 79101, "Я использую PowerShell.")
	path := "/messages/" + message.ID + "/memory-proposals"
	v := hrResponse[personalmemory.Proposals](t, h, "POST", path, map[string]any{}, intern, 200)
	if v.State != "failed" || len(v.Items) != 0 {
		t.Fatal(v)
	}
	w := hrResponse[personalmemory.Workspace](t, h, "GET", "/memory", nil, intern, 200)
	if len(w.Entries) != 0 {
		t.Fatal(w)
	}
	memoryChat(t, h, intern, 79103, "Как проверить текущую папку?")
	for _, item := range []struct {
		method, path string
		body         any
	}{
		{"POST", path, map[string]string{"user_id": "other"}}, {"PUT", "/memory/proposals/missing", map[string]any{"decision": "accept", "unknown": true}},
		{"PUT", "/memory/proposals/missing", map[string]string{"decision": "auto"}},
		{"PUT", "/memory/entries/missing", map[string]any{"value": "secret", "revision": 0}},
		{"DELETE", "/memory/entries/missing", map[string]int{"revision": 0}},
		{"PUT", "/memory/entries/missing", map[string]any{"value": 123, "revision": 1}},
		{"DELETE", "/memory/entries/missing", map[string]string{"revision": "invalid"}},
	} {
		hrResponse[any](t, h, item.method, item.path, item.body, intern, 422)
	}
	apiRequest(t, h, "POST", "/api/v1"+path, `broken`, intern, 400)
	hrResponse[any](t, h, "GET", "/messages/missing/memory-proposals", nil, intern, 404)
}

type memoryProfileProbe struct{ Seen chan string }

func (p memoryProfileProbe) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	p.Seen <- in.PersonalProfile
	return ports.AgentResult{Outcome: ports.OutcomeAnswered}, emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: "Контекст загружен."})
}
