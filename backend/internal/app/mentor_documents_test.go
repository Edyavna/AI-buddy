package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type uploadEmbedder struct {
	fail             bool
	entered, release chan struct{}
}

func (e uploadEmbedder) Embed(ctx context.Context, _, _ string) ([]float64, error) {
	if e.entered != nil {
		close(e.entered)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-e.release:
		}
	}
	if e.fail {
		return nil, errors.New("provider secret")
	}
	v := make([]float64, 1024)
	v[0] = 1
	return v, nil
}

func postMarkdown(t *testing.T, h http.Handler, cookie *http.Cookie, key, question, name, content string, code int) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range map[string]string{"upload_id": key, "title": "Правила дежурств", "question_id": question} {
		if v != "" {
			if err := form.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mentor/documents", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Origin", "http://localhost:5173")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != code {
		t.Fatalf("upload status = %d, want %d: %s", rec.Code, code, rec.Body.String())
	}
	return rec
}

func TestPostgresMentorDocumentLifecycle(t *testing.T) {
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	var assistant string
	if err := db.QueryRow("UPDATE chat_messages SET outcome='knowledge_gap' WHERE conversation_id=$1 AND role='assistant' RETURNING id", conversation).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	service := &knowledge.Service{DB: db, Embedder: uploadEmbedder{}}
	deps := Dependencies{Knowledge: service}
	h := requestHandler(db, deps)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	other := loginCookie(t, h, "mentor_demo_2", nil)
	otherTeam := loginCookie(t, h, "intern_demo_other", nil)
	var q knowledgegaps.Status
	transfer := apiRequest(t, h, "POST", "/api/v1/messages/"+assistant+"/knowledge-gap", "", student, 201)
	if err := json.Unmarshal(transfer.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	const key = "931ce14c-6248-48d7-995a-0087cf427d34"
	const content = "# Дежурства\nГрафик публикуется по понедельникам."
	postMarkdown(t, h, student, key, q.ID, "rules.md", content, 403)
	postMarkdown(t, h, other, key, q.ID, "rules.md", content, 404)
	postMarkdown(t, h, mentor, key, q.ID, "rules.md", string([]byte{0xff}), 422)
	postMarkdown(t, h, mentor, key, q.ID, "rules.md", strings.Repeat("x", knowledge.MaxMarkdownBytes+1), 422)
	postMarkdown(t, h, mentor, key, q.ID, "rules.txt", content, 422)
	created := postMarkdown(t, h, mentor, key, q.ID, "rules.md", content, 202)
	var d knowledge.Upload
	if err := json.Unmarshal(created.Body.Bytes(), &d); err != nil || d.Status != "processing" || d.QuestionID == nil || *d.QuestionID != q.ID {
		t.Fatal(created.Body.String(), err)
	}
	duplicate := postMarkdown(t, h, mentor, key, q.ID, "rules.md", content, 200)
	if duplicate.Body.String() != created.Body.String() {
		t.Fatal(duplicate.Body.String())
	}
	postMarkdown(t, h, mentor, key, q.ID, "rules.md", content+"!", 409)
	postMarkdown(t, h, mentor, key, "", "rules.md", content, 409)
	manage := "/api/v1/mentor/documents/" + d.ID
	shared := "/api/v1/documents/" + d.ID
	question := "/api/v1/mentor/questions/" + q.ID
	resolve := fmt.Sprintf(`{"status":"resolved","revision":1,"resolution":"График в инструкции","document_ids":[%q]}`, d.ID)
	apiRequest(t, h, "GET", manage, "", other, 404)
	apiRequest(t, h, "GET", shared, "", student, 404)
	apiRequest(t, h, "PATCH", question, resolve, mentor, 409)
	ctx := testkit.Context(context.Background())
	failed := knowledge.UploadWorker{Knowledge: &knowledge.Service{DB: db, Embedder: uploadEmbedder{fail: true}}}
	if processed, err := failed.RunOne(ctx); !processed || err != nil {
		t.Fatal(processed, err)
	}
	failure := apiRequest(t, h, "GET", manage, "", mentor, 200).Body.String()
	if !strings.Contains(failure, `"status":"failed"`) || strings.Contains(failure, "provider secret") {
		t.Fatal(failure)
	}
	apiRequest(t, h, "PATCH", question, resolve, mentor, 409)

	var wg sync.WaitGroup
	uploads := knowledge.Uploads{DB: db}
	mentorCtx := identity.WithPrincipal(ctx, identity.Principal{
		UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor,
	})
	for range 2 {
		wg.Go(func() {
			if got, err := uploads.Retry(mentorCtx, d.ID); err != nil || got.Status != "processing" {
				t.Errorf("retry = %q, %v", got.Status, err)
			}
		})
	}
	wg.Wait()
	apiRequest(t, h, "POST", manage+"/retry", "", mentor, 202)
	started, release := make(chan struct{}), make(chan struct{})
	interrupted := knowledge.UploadWorker{Knowledge: &knowledge.Service{DB: db, Embedder: uploadEmbedder{entered: started, release: release}}}
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _, _ = interrupted.RunOne(cancelCtx) }()
	select {
	case <-started:
	case <-done:
		t.Fatal("indexing worker exited before claiming the document")
	}
	if processed, err := interrupted.RunOne(ctx); processed || err != nil {
		t.Fatal(processed, err)
	}
	apiRequest(t, h, "GET", shared, "", student, 404)
	apiRequest(t, h, "PATCH", question, resolve, mentor, 409)
	cancel()
	<-done

	worker := knowledge.UploadWorker{Knowledge: service}
	if err := worker.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.RunOne(ctx); !processed || err != nil {
		t.Fatal(processed, err)
	}
	if processed, err := worker.RunOne(ctx); processed || err != nil {
		t.Fatal(processed, err)
	}
	h = requestHandler(db, deps)
	detail := apiRequest(t, h, "GET", manage, "", mentor, 200).Body.String()
	if !strings.Contains(detail, `"status":"ready"`) || !strings.Contains(detail, "понедельникам") {
		t.Fatal(detail)
	}
	apiRequest(t, h, "GET", "/api/v1/mentor/documents", "", mentor, 200)
	apiRequest(t, h, "GET", manage, "", other, 404)
	apiRequest(t, h, "GET", shared, "", otherTeam, 404)
	sharedDocument := apiRequest(t, h, "GET", shared, "", student, 200)
	var timestamp struct {
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(sharedDocument.Body.Bytes(), &timestamp); err != nil || !strings.HasSuffix(timestamp.UpdatedAt, "Z") {
		t.Fatal(sharedDocument.Body.String(), err)
	}
	apiRequest(t, h, "POST", manage+"/retry", "", mentor, 409)
	apiRequest(t, h, "PATCH", question, resolve, mentor, 200)

	replies := []agent.Message{
		{Calls: []agent.Call{{ID: "search", Type: "function", Function: agent.Function{Name: "search_knowledge", Arguments: `{"query":"График дежурств"}`}}}},
		{Calls: []agent.Call{{ID: "read", Type: "function", Function: agent.Function{Name: "read_document", Arguments: fmt.Sprintf(`{"document_id":%q}`, d.ID)}}}},
		{Content: `{"answer":"График публикуется по понедельникам.","outcome":"answered"}`},
	}
	a := agent.Agent{Model: &agent.Script{Replies: replies}, Knowledge: service, Tasks: testkit.Tasks{}, Access: &testkit.Access{}, Requests: testkit.Requests{}, Drafts: testkit.Drafts(func(context.Context, ports.DraftInput) (ports.Draft, error) { return ports.Draft{}, ports.ErrInvalid })}
	result, err := a.Run(ctx, ports.AgentInput{ConversationID: conversation, AssistantMessageID: "next", Text: "Где график дежурств?"}, func(context.Context, ports.AgentEvent) error { return nil })
	if err != nil || result.Outcome != ports.OutcomeAnswered || len(result.Sources) != 1 || result.Sources[0].DocumentID != d.ID {
		t.Fatal(result, err)
	}
}
