package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/drafts"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

const (
	requestID   = "integration-request"
	requestURL  = "/api/v1/access-requests/" + requestID
	confirmURL  = requestURL + "/confirm"
	confirmBody = `{"draft_version":1}`
	planURL     = "/api/v1/onboarding/plan"
)

type draftAgent struct {
	drafts ports.DraftService
	tasks  ports.TaskProvider
}

func (a draftAgent) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	task, err := a.tasks.FirstTask(ctx)
	if err != nil {
		return ports.AgentResult{}, err
	}
	draft, err := a.drafts.SaveAndLink(ctx, ports.DraftInput{
		RequestID: requestID, ConversationID: in.ConversationID, AssistantMessageID: in.AssistantMessageID,
		StepID: in.StepID, Resource: task.Resource, Reason: "Доступ для учебной задачи",
	})
	if err != nil {
		return ports.AgentResult{}, err
	}
	return ports.AgentResult{RequestIDs: []string{draft.RequestID}},
		emit(ctx, ports.AgentEvent{Kind: "draft.saved", Draft: &draft})
}

func TestPostgresAccessRequestLifecycle(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	h, cookie, conversation := prepareDraft(t, db)
	store := accessrequests.Store{DB: db}
	provider := integrations.New(db)
	control, err := integrations.NewControl(db, true)
	if err != nil {
		t.Fatal(err)
	}
	assertExternalCount(t, control, 0)

	draft := getRequest(t, h, cookie)
	if draft.Status != "draft" || draft.ConversationID != conversation || draft.Version != 1 ||
		draft.Revision != 1 || draft.ExternalID != nil || draft.CheckedAt != nil {
		t.Fatalf("draft: %+v", draft)
	}
	apiRequest(t, h, "GET", requestURL, "", nil, http.StatusUnauthorized)
	other := loginCookie(t, h, "intern_demo_2", nil)
	if body := apiRequest(t, h, "GET", "/api/v1/access-requests", "", other, 200).Body.String(); body != "[]\n" {
		t.Fatal(body)
	}
	apiRequest(t, h, "GET", requestURL, "", other, 404)
	apiRequest(t, h, "POST", confirmURL, confirmBody, other, 404)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	apiRequest(t, h, "GET", requestURL, "", mentor, 403)
	apiRequest(t, h, "POST", confirmURL, `{"draft_version":0}`, cookie, 422)
	w := apiRequest(t, h, "POST", confirmURL, `{"draft_version":2}`, cookie, 409)
	if !strings.Contains(w.Body.String(), "DRAFT_CHANGED") {
		t.Fatal(w.Body.String())
	}
	r := httptest.NewRequest("POST", confirmURL, strings.NewReader(confirmBody))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://example.invalid")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	assertExternalCount(t, control, 0)

	var wg sync.WaitGroup
	var codes [2]int
	for i := range codes {
		wg.Go(func() {
			r := httptest.NewRequest("POST", confirmURL, strings.NewReader(confirmBody))
			r.AddCookie(cookie)
			r.Header.Set("Origin", "http://localhost:5173")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			codes[i] = w.Code
		})
	}
	wg.Wait()
	if codes != [2]int{202, 202} {
		t.Fatal(codes)
	}
	submitting := getRequest(t, h, cookie)
	if submitting.Status != "submitting" || submitting.Revision != 2 || submitting.Version != 1 {
		t.Fatalf("accepted twice or not accepted: %+v", submitting)
	}
	if plan := decodePlan(t, apiRequest(t, h, "GET", planURL, "", cookie, 200)); plan.Steps[0].Status != "blocked" {
		t.Fatal(plan)
	}
	assertExternalCount(t, control, 0)
	if err := control.SetFault(ctx, requestID, "timeout-after-commit"); err != nil {
		t.Fatal(err)
	}
	worker := accessrequests.Worker{Store: store, Provider: provider}
	if worked, err := worker.RunOne(ctx); !worked || !errors.Is(err, ports.ErrOutcomeUnknown) {
		t.Fatalf("expected unknown outcome: %v %v", worked, err)
	}
	assertExternalCount(t, control, 1)
	if current := getRequest(t, h, cookie); current.Status != "submitting" || current.CheckedAt != nil || current.Revision != submitting.Revision {
		t.Fatalf("unknown outcome changed persisted state: %+v", current)
	}

	// A new worker and a new handler recover the existing job after the lease.
	dueNow(t, db)
	restarted := accessrequests.Worker{Store: accessrequests.Store{DB: db}, Provider: integrations.New(db)}
	runDue(t, ctx, restarted)
	h = requestHandler(db, Dependencies{})
	pending := getRequest(t, h, cookie)
	if pending.Status != "pending" || pending.ExternalID == nil || pending.CheckedAt == nil || pending.Revision <= submitting.Revision {
		t.Fatalf("lost reply was not reconciled: %+v", pending)
	}
	w = apiRequest(t, h, "POST", confirmURL, `{"draft_version":2}`, cookie, 200)
	if got := decodeRequest(t, w); got.ExternalID == nil || *got.ExternalID != *pending.ExternalID || got.Revision != pending.Revision {
		t.Fatalf("repeat changed pending request: %+v", got)
	}
	w = apiRequest(t, h, "GET", "/api/v1/conversations/"+conversation+"/messages", "", cookie, 200)
	var messages []chat.Message
	if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil || len(messages) != 2 || !slices.Contains(messages[1].RequestIDs, requestID) {
		t.Fatal(err, messages)
	}
	if err := control.SetFault(ctx, requestID, "lookup-unavailable"); err != nil {
		t.Fatal(err)
	}
	dueNow(t, db)
	if worked, err := restarted.RunOne(ctx); !worked || !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("unavailable lookup: %v %v", worked, err)
	}
	if got := getRequest(t, h, cookie); got.Revision != pending.Revision || !got.CheckedAt.Equal(*pending.CheckedAt) {
		t.Fatalf("failed lookup changed freshness: %+v", got)
	}
	if err := control.SetFault(ctx, requestID, "none"); err != nil {
		t.Fatal(err)
	}
	if err := control.Decide(ctx, requestID, "granted", ""); err != nil {
		t.Fatal(err)
	}
	dueNow(t, db)
	runDue(t, ctx, restarted)
	granted := getRequest(t, h, cookie)
	if granted.Status != "granted" || granted.ExternalID == nil || *granted.ExternalID != *pending.ExternalID {
		t.Fatalf("grant not reconciled: %+v", granted)
	}
	if plan := decodePlan(t, apiRequest(t, h, "GET", planURL, "", cookie, 200)); plan.Steps[0].Status != "in_progress" || plan.ProgressPercent != 0 {
		t.Fatal(plan)
	}
	plan := decodePlan(t, apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, cookie, 200))
	if plan.ProgressPercent != 100 {
		t.Fatal(plan)
	}
	assertExternalCount(t, control, 1)
}

func TestPostgresAccessRequestFailedRetryAndRejection(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	h, cookie, _ := prepareDraft(t, db)
	control, err := integrations.NewControl(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := control.SetFault(ctx, requestID, "reject-before-write"); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "POST", confirmURL, confirmBody, cookie, 202)
	worker := accessrequests.Worker{Store: accessrequests.Store{DB: db}, Provider: integrations.New(db)}
	runDue(t, ctx, worker)
	failed := getRequest(t, h, cookie)
	if failed.Status != "failed" || failed.ExternalID != nil || failed.FailureReason == "" {
		t.Fatalf("expected known failure: %+v", failed)
	}
	assertExternalCount(t, control, 0)
	apiRequest(t, h, "POST", confirmURL, `{"draft_version":2}`, cookie, 409)
	if err := control.SetFault(ctx, requestID, "none"); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "POST", confirmURL, confirmBody, cookie, 202)
	runDue(t, ctx, worker)
	assertExternalCount(t, control, 1)
	if err := control.Decide(ctx, requestID, "rejected", "sensitive provider text"); err != nil {
		t.Fatal(err)
	}
	dueNow(t, db)
	runDue(t, ctx, worker)
	rejected := decodeRequest(t, apiRequest(t, h, "POST", confirmURL, confirmBody, cookie, 200))
	if rejected.Status != "rejected" || rejected.ExternalID == nil || rejected.FailureReason == "" ||
		strings.Contains(rejected.FailureReason, "sensitive") {
		t.Fatalf("expected safe rejection: %+v", rejected)
	}
	if plan := decodePlan(t, apiRequest(t, h, "GET", planURL, "", cookie, 200)); plan.Steps[0].Status != "blocked" {
		t.Fatal(plan)
	}
	assertExternalCount(t, control, 1)
}

func prepareDraft(t *testing.T, db *sql.DB) (http.Handler, *http.Cookie, string) {
	t.Helper()
	if err := demo.Seed(context.Background(), db, true); err != nil {
		t.Fatal(err)
	}
	p := integrations.New(db)
	h := requestHandler(db, Dependencies{Access: p, Agent: draftAgent{drafts.New(accessrequests.Store{DB: db}), p}})
	cookie := loginCookie(t, h, testkit.UserID, nil)
	w := apiRequest(t, h, "POST", "/api/v1/conversations", `{"client_conversation_id":"99999999-1111-4444-8888-000000000001","step_id":"step_local_setup"}`, cookie, 201)
	var c chat.Conversation
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil || c.ID == "" {
		t.Fatal(err, c)
	}
	r := httptest.NewRequest("POST", "/api/v1/conversations/"+c.ID+"/messages",
		strings.NewReader(`{"client_message_id":"99999999-1111-4444-8888-000000000002","text":"Как получить доступ?"}`))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	stream := w.Body.String()
	proposed, completed := strings.Index(stream, "event: action.proposed"), strings.Index(stream, "event: message.completed")
	if w.Code != 200 || proposed < 0 || completed <= proposed {
		t.Fatal(w.Code, stream)
	}
	return h, cookie, c.ID
}

func requestHandler(db *sql.DB, deps Dependencies) http.Handler {
	cfg := config.Config{DemoMode: true, SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"}}
	return NewHandler(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, deps)
}

func getRequest(t *testing.T, h http.Handler, cookie *http.Cookie) httpapi.AccessRequest {
	t.Helper()
	return decodeRequest(t, apiRequest(t, h, "GET", requestURL, "", cookie, 200))
}

func decodeRequest(t *testing.T, w *httptest.ResponseRecorder) httpapi.AccessRequest {
	t.Helper()
	var request httpapi.AccessRequest
	if err := json.Unmarshal(w.Body.Bytes(), &request); err != nil || request.ID != requestID {
		t.Fatal(err, w.Body.String())
	}
	return request
}

func assertExternalCount(t *testing.T, control *integrations.Control, want int) {
	t.Helper()
	if got, err := control.Count(context.Background()); err != nil || got != want {
		t.Fatalf("external count: want %d, got %d: %v", want, got, err)
	}
}

func dueNow(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`UPDATE access_request_jobs SET next_attempt_at=now() WHERE request_id=$1`, requestID); err != nil {
		t.Fatal(err)
	}
}

func runDue(t *testing.T, ctx context.Context, worker accessrequests.Worker) {
	t.Helper()
	if worked, err := worker.RunOne(ctx); !worked || err != nil {
		t.Fatalf("worker: worked=%v err=%v", worked, err)
	}
}
