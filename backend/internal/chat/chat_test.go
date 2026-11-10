package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type agentFunc func(context.Context, ports.AgentInput, ports.Emit) (ports.AgentResult, error)

func (f agentFunc) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	return f(ctx, in, emit)
}
func uid(n int) string { return fmt.Sprintf("abcdef01-1111-4111-8111-%012x", n) }
func testOwner() identity.Principal {
	return identity.Principal{UserID: testkit.UserID, TeamID: testkit.TeamID, Role: identity.Intern}
}
func testContext() context.Context { return testkit.Context(context.Background()) }

type fixture struct {
	db      *sql.DB
	store   SQLStore
	service Service
	handler http.Handler
	cookie  *http.Cookie
}

func newFixture(t *testing.T, agent ports.Agent, requests ports.RequestReader, o StreamOptions) *fixture {
	t.Helper()
	db := testkit.Postgres(t)
	if err := demo.Seed(context.Background(), db, true); err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, store: SQLStore{DB: db}}
	f.service = Service{Store: f.store, Agent: agent, Requests: requests}
	session := auth.Handler{Service: &auth.Service{Store: auth.SQLStore{DB: db}, TTL: time.Hour}, DemoMode: true}
	f.handler = httpapi.New(httpapi.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AllowedOrigins: []string{"http://localhost:5173"}, Register: func(mux *http.ServeMux) {
		session.Register(mux)
		Handler{Service: f.service, Options: o}.Register(mux, session)
	}})
	f.cookie = f.login(t, "intern_demo_1")
	return f
}
func (f *fixture) call(t *testing.T, method, path, body string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s want %d got %d: %s", method, path, want, w.Code, w.Body.String())
	}
	return w
}
func (f *fixture) login(t *testing.T, id string) *http.Cookie {
	return f.call(t, "POST", "/demo/session", `{"account_id":"`+id+`"}`, nil, 200).Result().Cookies()[0]
}
func (f *fixture) create(t *testing.T, n int) Conversation {
	t.Helper()
	w := f.call(t, "POST", "/conversations", `{"client_conversation_id":"`+uid(n)+`","step_id":"step_local_setup"}`, f.cookie, 201)
	var c Conversation
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func decodeMessages(t *testing.T, w *httptest.ResponseRecorder) []Message {
	t.Helper()
	var m []Message
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("null messages")
	}
	for _, row := range m {
		if row.Sources == nil || row.RequestIDs == nil {
			t.Fatal("null message arrays")
		}
	}
	return m
}
func fastAgent() ports.Agent {
	return agentFunc(func(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
		for _, text := range []string{"Привет", " 🚀\n"} {
			if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: text}); err != nil {
				return ports.AgentResult{}, err
			}
		}
		return ports.AgentResult{}, nil
	})
}
func TestPostgresChatAPI(t *testing.T) {
	var calls atomic.Int32
	inputs := make(chan ports.AgentInput, 8)
	agent := agentFunc(func(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
		calls.Add(1)
		inputs <- in
		return fastAgent().Run(ctx, in, emit)
	})
	f := newFixture(t, agent, nil, StreamOptions{})
	if w := f.call(t, "GET", "/conversations", "", f.cookie, 200); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Body.String())
	}
	f.call(t, "GET", "/conversations", "", nil, 401)
	for _, body := range []string{`{}`, `{"client_conversation_id":null}`, `{"client_conversation_id":"bad"}`, `{"client_conversation_id":"` + uid(1) + `","user_id":"foreign"}`, `{"client_conversation_id":"` + uid(1) + `","step_id":12}`} {
		f.call(t, "POST", "/conversations", body, f.cookie, 422)
	}
	c := f.create(t, 1)
	if c.Title != "Запустить проект" || c.StepID == nil || *c.StepID != "step_local_setup" {
		t.Fatal(c)
	}
	f.call(t, "GET", "/conversations/"+c.ID, "", f.cookie, 200)
	repeat := f.call(t, "POST", "/conversations", `{"client_conversation_id":"`+strings.ToUpper(uid(1))+`","step_id":"step_local_setup"}`, f.cookie, 200)
	if !strings.Contains(repeat.Body.String(), c.ID) {
		t.Fatal("repeat created another conversation")
	}
	f.call(t, "POST", "/conversations", `{"client_conversation_id":"`+uid(1)+`"}`, f.cookie, 409)
	f.call(t, "POST", "/conversations", `{"client_conversation_id":"`+uid(2)+`","step_id":"step_local_setup_2"}`, f.cookie, 404)
	for _, body := range []string{`{"client_conversation_id":"` + uid(3) + `"}`, `{"client_conversation_id":"` + uid(3) + `","step_id":null}`} {
		want := 201
		if strings.Contains(body, "null") {
			want = 200
		}
		f.call(t, "POST", "/conversations", body, f.cookie, want)
	}
	path := "/conversations/" + c.ID + "/messages"
	if len(decodeMessages(t, f.call(t, "GET", path, "", f.cookie, 200))) != 0 {
		t.Fatal("GET created messages")
	}
	for _, body := range []string{`{}`, `{"client_message_id":"` + uid(10) + `","text":null}`, `{"client_message_id":"bad","text":"Hi"}`, `{"client_message_id":"` + uid(10) + `","text":"  "}`, `{"client_message_id":"` + uid(10) + `","text":"Hi","team_id":"forged"}`} {
		f.call(t, "POST", path, body, f.cookie, 422)
	}
	text := "  Вопрос? \n"
	body, _ := json.Marshal(map[string]string{"client_message_id": uid(10), "text": text})
	stream := f.call(t, "POST", path, string(body), f.cookie, 200)
	if stream.Header().Get("Content-Type") != "text/event-stream" || stream.Header().Get("X-Accel-Buffering") != "no" || !stream.Flushed || stream.Header().Get("Cache-Control") != "no-cache, no-store" {
		t.Fatal(stream.Header())
	}
	frames := strings.Split(strings.TrimSpace(stream.Body.String()), "\n\n")
	if !strings.HasPrefix(frames[0], "event: message.started\n") || !strings.HasPrefix(frames[len(frames)-1], "event: message.completed\n") {
		t.Fatal(stream.Body.String())
	}
	in := <-inputs
	if in.Text != text || len(in.History) != 0 {
		t.Fatal(in)
	}
	all := decodeMessages(t, f.call(t, "GET", path, "", f.cookie, 200))
	if len(all) != 2 || all[0].Text != text || all[0].Status != "completed" || all[1].Text != "Привет 🚀\n" || all[1].Status != "completed" || all[1].ClientID != nil || all[1].InReplyTo == nil || *all[1].InReplyTo != all[0].ID {
		t.Fatal(all)
	}
	for _, tt := range []struct {
		text string
		code string
	}{{text, "MESSAGE_ALREADY_EXISTS"}, {strings.TrimSpace(text), "IDEMPOTENCY_CONFLICT"}} {
		body, _ := json.Marshal(map[string]string{"client_message_id": uid(10), "text": tt.text})
		w := f.call(t, "POST", path, string(body), f.cookie, 409)
		if !strings.Contains(w.Body.String(), tt.code) {
			t.Fatal(w.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate called agent")
	}
	body, _ = json.Marshal(map[string]string{"client_message_id": uid(11), "text": strings.Repeat("🚀", 8001)})
	f.call(t, "POST", path, string(body), f.cookie, 422)
	body, _ = json.Marshal(map[string]string{"client_message_id": uid(11), "text": strings.Repeat("🚀", 8000)})
	f.call(t, "POST", path, string(body), f.cookie, 200)
	in = <-inputs
	if len(in.History) != 2 || in.History[0].Text != text {
		t.Fatal(in)
	}
	// Equal timestamps cannot reorder user/assistant pairs.
	if _, err := f.db.Exec(`UPDATE chat_messages SET created_at='2026-11-10T00:00:00Z' WHERE conversation_id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	all = decodeMessages(t, f.call(t, "GET", path, "", f.cookie, 200))
	if len(all) != 4 || all[2].Role != "user" || all[3].Role != "assistant" {
		t.Fatal(all)
	}
	for _, id := range []string{"intern_demo_2", "intern_demo_other"} {
		cookie := f.login(t, id)
		f.call(t, "GET", "/conversations/"+c.ID, "", cookie, 404)
		f.call(t, "GET", path, "", cookie, 404)
		f.call(t, "POST", path, `{`, cookie, 404) // Ownership precedes malformed JSON.
		if w := f.call(t, "GET", "/conversations", "", cookie, 200); strings.TrimSpace(w.Body.String()) != "[]" {
			t.Fatal("leaked conversations")
		}
	}
	mentor := f.login(t, "mentor_demo_1")
	for _, route := range []string{"/conversations", "/conversations/" + c.ID, path} {
		f.call(t, "GET", route, "", mentor, 403)
	}
	f.call(t, "POST", "/conversations", `{}`, mentor, 403)
	f.call(t, "POST", path, `{}`, mentor, 403)
}
func TestPostgresReservationConcurrencyAndRecovery(t *testing.T) {
	f := newFixture(t, fastAgent(), nil, StreamOptions{})
	c := f.create(t, 1)
	ctx := testContext()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, n := range []int{10, 11} {
		go func(n int) { <-start; _, err := f.service.Prepare(ctx, c.ID, uid(n), "Вопрос"); results <- err }(n)
	}
	close(start)
	accepted, busy := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || busy != 1 {
		t.Fatal(accepted, busy)
	}
	all, err := f.store.Messages(ctx, testOwner(), c.ID)
	if err != nil || len(all) != 2 {
		t.Fatal(all, err)
	}
	id := *all[0].ClientID
	if _, err := f.service.Prepare(ctx, c.ID, id, "Вопрос"); !errors.Is(err, ErrDuplicate) {
		t.Fatal(err)
	}
	if _, err := f.service.Prepare(ctx, c.ID, id, "Другой вопрос"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal(err)
	}
	f.call(t, "POST", "/conversations/"+c.ID+"/messages", `{"client_message_id":"`+uid(12)+`","text":"New"}`, f.cookie, 409)
	if err := f.store.SavePartial(ctx, testOwner(), c.ID, all[1].ID, "Часть ответа"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	all, err = f.store.Messages(ctx, testOwner(), c.ID)
	if err != nil || all[1].Status != "interrupted" || all[1].Text != "Часть ответа" || all[1].Error.Error.Code != "GENERATION_INTERRUPTED" {
		t.Fatal(all, err)
	}
	if _, err := f.service.Prepare(ctx, c.ID, uid(12), "После рестарта"); err != nil {
		t.Fatal(err)
	}
	// Concurrent create requests resolve to exactly one 201 and one 200.
	created := make(chan bool, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, first, err := f.service.Create(ctx, uid(99), nil); created <- first; failures <- err })
	}
	wg.Wait()
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	if a, b := <-created, <-created; a == b {
		t.Fatal("create was not idempotent")
	}
}
func TestMissingAgentDoesNotAcceptMessage(t *testing.T) {
	f := newFixture(t, nil, nil, StreamOptions{})
	c := f.create(t, 1)
	f.call(t, "POST", "/conversations/"+c.ID+"/messages", `{"client_message_id":"`+uid(1)+`","text":"Hi"}`, f.cookie, 503)
	if m := decodeMessages(t, f.call(t, "GET", "/conversations/"+c.ID+"/messages", "", f.cookie, 200)); len(m) != 0 {
		t.Fatal("accepted without agent")
	}
}
func TestHistoryBudgetAndValidation(t *testing.T) {
	messages := make([]Message, 0)
	for n := range 20 {
		u := fmt.Sprint(n)
		messages = append(messages, Message{ID: u, Role: "user", Text: "Вопрос", Status: "completed"}, Message{Role: "assistant", Text: "Ответ", Status: "completed", InReplyTo: &u})
	}
	h := history(messages, "Текущий")
	if len(h) != 18 || h[len(h)-2].Text != "Вопрос" {
		t.Fatal(h)
	}
	messages[len(messages)-1].Text = strings.Repeat("я", 24000)
	if h := history(messages, "Now"); len(h) != 0 {
		t.Fatal("split an oversized latest pair")
	}
	if validText("\n\t ") || validUUID("no") || !validUUID(strings.ToUpper(uid(1))) {
		t.Fatal("validation")
	}
}
