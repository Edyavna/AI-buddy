package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

// Only embeddings are synthetic. Upload, indexing, ACL, CheckReady and PATCH
// use the production implementations and real PostgreSQL transactions.
type gapPatchFixture struct {
	db              *sql.DB
	handler         http.Handler
	mentor, student *http.Cookie
	question        knowledgegaps.Status
	path            string
	uploadNumber    int
}

func newGapPatchFixture(t *testing.T) *gapPatchFixture {
	t.Helper()
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	var message string
	if err := db.QueryRow(`UPDATE chat_messages SET outcome='knowledge_gap' WHERE conversation_id=$1 AND role='assistant' RETURNING id`, conversation).Scan(&message); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	f := &gapPatchFixture{db: db, handler: h, student: student, mentor: loginCookie(t, h, "mentor_demo_1", nil)}
	reply := apiRequest(t, h, "POST", "/api/v1/messages/"+message+"/knowledge-gap", "", student, 201)
	if err := json.Unmarshal(reply.Body.Bytes(), &f.question); err != nil {
		t.Fatal(err)
	}
	f.path = "/api/v1/mentor/questions/" + f.question.ID
	return f
}

func (f *gapPatchFixture) upload(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	f.uploadNumber++
	key := fmt.Sprintf("b3700000-0000-4000-8000-%012d", f.uploadNumber)
	reply := postMarkdown(t, f.handler, cookie, key, "", "rules.md", "# Правила\nПроверенная инструкция.", 202)
	var d knowledge.Upload
	if err := json.Unmarshal(reply.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	worker := knowledge.UploadWorker{Knowledge: &knowledge.Service{DB: f.db, Embedder: uploadEmbedder{}}}
	if worked, err := worker.RunOne(t.Context()); !worked || err != nil {
		t.Fatalf("indexing: %v %v", worked, err)
	}
	var status string
	if err := f.db.QueryRow(`SELECT status FROM mentor_documents WHERE id=$1`, d.ID).Scan(&status); err != nil || status != "ready" {
		t.Fatalf("document status: %s %v", status, err)
	}
	return d.ID
}

func gapResolution(revision int64, text string, ids ...string) string {
	b, err := json.Marshal(map[string]any{"status": "resolved", "revision": revision, "resolution": text, "document_ids": ids})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func gapRequest(ctx context.Context, h http.Handler, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PATCH", path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")
	req.AddCookie(cookie)
	reply := httptest.NewRecorder()
	h.ServeHTTP(reply, req)
	return reply
}

// Include persisted timestamps, links and the complete history, not just the DTO.
func gapSnapshot(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var result string
	err := db.QueryRow(`SELECT jsonb_build_object('question',to_jsonb(q),'history',
 COALESCE((SELECT jsonb_agg(to_jsonb(h) ORDER BY h.revision) FROM knowledge_gap_history h WHERE h.question_id=q.id),'[]'::jsonb))::text
 FROM knowledge_gaps q WHERE q.id=$1`, id).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertGapUnchanged(t *testing.T, f *gapPatchFixture, before string) {
	t.Helper()
	if after := gapSnapshot(t, f.db, f.question.ID); after != before {
		t.Fatalf("failed/no-op request changed persisted state\nbefore: %s\nafter: %s", before, after)
	}
}

// Observe an actual PostgreSQL wait instead of guessing with a sleep. The SQL
// predicate excludes this observer connection and is scoped to the isolated DB.
func waitGapLocks(t *testing.T, db *sql.DB, pattern string, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
 AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1`, pattern).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wanted %d blocked queries matching %s, got %d", want, pattern, count)
		case <-tick.C:
		}
	}
}

func TestPostgresKnowledgeGapConcurrentResolutions(t *testing.T) {
	f := newGapPatchFixture(t)
	first, second := f.upload(t, f.mentor), f.upload(t, f.mentor)
	tx, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(t.Context(), `SELECT id FROM knowledge_gaps WHERE id=$1 FOR UPDATE`, f.question.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	replies := make(chan *httptest.ResponseRecorder, 2)
	for i, id := range []string{first, second} {
		body := gapResolution(1, fmt.Sprintf("Решение %d", i+1), id)
		go func() { replies <- gapRequest(ctx, f.handler, f.path, body, f.mentor) }()
	}
	waitGapLocks(t, f.db, "%FOR UPDATE OF q%", 2)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var winner knowledgegaps.Question
	successes, conflicts := 0, 0
	for range 2 {
		var reply *httptest.ResponseRecorder
		select {
		case reply = <-replies:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		switch reply.Code {
		case 200:
			successes++
			if err = json.Unmarshal(reply.Body.Bytes(), &winner); err != nil {
				t.Fatal(err)
			}
		case 409:
			conflicts++
			if !strings.Contains(reply.Body.String(), "QUESTION_CHANGED") {
				t.Fatal(reply.Body.String())
			}
		default:
			t.Fatalf("unexpected response %d: %s", reply.Code, reply.Body.String())
		}
	}
	if successes != 1 || conflicts != 1 || winner.Revision != 2 {
		t.Fatalf("success=%d conflict=%d winner=%+v", successes, conflicts, winner)
	}
	if len(winner.DocumentIDs) != 1 || winner.Resolution == nil {
		t.Fatal(winner)
	}
	expectedText := "Решение 1"
	if winner.DocumentIDs[0] == second {
		expectedText = "Решение 2"
	} else if winner.DocumentIDs[0] != first {
		t.Fatal(winner)
	}
	if *winner.Resolution != expectedText {
		t.Fatal("mixed decisions", winner)
	}
	current := apiRequest(t, requestHandler(f.db, Dependencies{}), "GET", f.path, "", f.mentor, 200)
	var persisted knowledgegaps.Question
	if err = json.Unmarshal(current.Body.Bytes(), &persisted); err != nil || !reflect.DeepEqual(winner, persisted) {
		t.Fatalf("winner overwritten: %+v %+v %v", winner, persisted, err)
	}
	var count int
	if err = f.db.QueryRow(`SELECT count(*) FROM knowledge_gap_history WHERE question_id=$1`, winner.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("history count %d: %v", count, err)
	}
}

func TestPostgresKnowledgeGapPatchIdempotencyAndHistory(t *testing.T) {
	f := newGapPatchFixture(t)
	doc := f.upload(t, f.mentor)
	apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Инструкция", doc), f.mentor, 200)
	saved := gapSnapshot(t, f.db, f.question.ID)
	// Current revision, identical payload: no new timestamp, history or revision.
	apiRequest(t, f.handler, "PATCH", f.path, gapResolution(2, "Инструкция", doc), f.mentor, 200)
	assertGapUnchanged(t, f, saved)
	// Lost response retry with the old revision is a conflict, followed by GET.
	reply := apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Инструкция", doc), f.mentor, 409)
	if !strings.Contains(reply.Body.String(), "QUESTION_CHANGED") {
		t.Fatal(reply.Body.String())
	}
	assertGapUnchanged(t, f, saved)
	reopened := apiRequest(t, f.handler, "PATCH", f.path, `{"status":"in_progress","revision":2}`, f.mentor, 200)
	var q knowledgegaps.Question
	if err := json.Unmarshal(reopened.Body.Bytes(), &q); err != nil || q.State != "in_progress" || q.Revision != 3 || q.Resolution == nil || *q.Resolution != "Инструкция" || !reflect.DeepEqual(q.DocumentIDs, []string{doc}) {
		t.Fatalf("reopen lost resolution: %+v %v", q, err)
	}
	var history knowledgegaps.Question
	var raw []byte
	if err := f.db.QueryRow(`SELECT snapshot FROM knowledge_gap_history WHERE question_id=$1 AND revision=2`, q.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &history); err != nil || history.State != "resolved" || history.Resolution == nil || *history.Resolution != "Инструкция" || !reflect.DeepEqual(history.DocumentIDs, []string{doc}) {
		t.Fatalf("history lost resolution: %+v %v", history, err)
	}
	saved = gapSnapshot(t, f.db, q.ID)
	apiRequest(t, f.handler, "PATCH", f.path, `{"status":"in_progress","revision":3}`, f.mentor, 200)
	assertGapUnchanged(t, f, saved)
	// Recreate all request services: persistence must not depend on handler memory.
	fresh := requestHandler(f.db, Dependencies{})
	if got := apiRequest(t, fresh, "GET", f.path, "", f.mentor, 200).Body.String(); got != reopened.Body.String() {
		t.Fatalf("fresh read differs: %s", got)
	}
	var status struct {
		Question knowledgegaps.Status `json:"question"`
	}
	reply = apiRequest(t, fresh, "GET", "/api/v1/messages/"+q.MessageID+"/knowledge-gap", "", f.student, 200)
	if err := json.Unmarshal(reply.Body.Bytes(), &status); err != nil || !reflect.DeepEqual(status.Question, q.Status) {
		t.Fatalf("student sees different state: %+v %v", status, err)
	}
}

func TestPostgresKnowledgeGapRejectsDocumentsAndUnauthorizedPatch(t *testing.T) {
	f := newGapPatchFixture(t)
	good, bad := f.upload(t, f.mentor), f.upload(t, f.mentor)
	saved := gapSnapshot(t, f.db, f.question.ID)
	for _, state := range []string{"processing", "failed"} {
		t.Run(state, func(t *testing.T) {
			if _, err := f.db.Exec(`UPDATE mentor_documents SET status=$1 WHERE id=$2`, state, bad); err != nil {
				t.Fatal(err)
			}
			reply := apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Не сохранить даже первый документ", good, bad), f.mentor, 409)
			if !strings.Contains(reply.Body.String(), "DOCUMENT_NOT_READY") {
				t.Fatal(reply.Body.String())
			}
			assertGapUnchanged(t, f, saved)
		})
	}
	if _, err := f.db.Exec(`UPDATE mentor_documents SET status='ready' WHERE id=$1`, bad); err != nil {
		t.Fatal(err)
	}
	for _, users := range []string{`["mentor_demo_1"]`, `["intern_demo_1"]`} {
		if _, err := f.db.Exec(`UPDATE knowledge_documents SET users=$1::jsonb WHERE id=$2`, users, bad); err != nil {
			t.Fatal(err)
		}
		apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Нет прав у одного участника", good, bad), f.mentor, 409)
		assertGapUnchanged(t, f, saved)
	}
	if _, err := f.db.Exec(`UPDATE knowledge_documents SET users='[]' WHERE id=$1`, bad); err != nil {
		t.Fatal(err)
	}
	other := loginCookie(t, f.handler, "mentor_demo_2", nil)
	foreign := f.upload(t, other)
	apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Документ другого автора", good, foreign), f.mentor, 409)
	assertGapUnchanged(t, f, saved)
	if _, err := f.db.Exec(`UPDATE knowledge_documents SET team_id='team_demo_frontend' WHERE id=$1`, bad); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Документ другой команды", good, bad), f.mentor, 409)
	assertGapUnchanged(t, f, saved)
	for _, item := range []struct {
		cookie *http.Cookie
		code   int
	}{{nil, 401}, {f.student, 403}, {other, 404}} {
		apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Нет полномочий", good), item.cookie, item.code)
		assertGapUnchanged(t, f, saved)
	}
	if _, err := f.db.Exec(`DELETE FROM mentor_assignments WHERE intern_id='intern_demo_1'`); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, f.handler, "GET", f.path, "", f.mentor, 404)
	apiRequest(t, f.handler, "PATCH", f.path, gapResolution(1, "Назначение отозвано", good), f.mentor, 404)
	assertGapUnchanged(t, f, saved)
}

// This is a timing gate around the real checker, not a replacement for its SQL.
type gatedGapDocuments struct {
	uploads knowledge.Uploads
	checked chan struct{}
	release chan struct{}
}

func (g gatedGapDocuments) CheckReady(ctx context.Context, tx *sql.Tx, mentor, student identity.Principal, ids []string) error {
	if err := g.uploads.CheckReady(ctx, tx, mentor, student, ids); err != nil {
		return err
	}
	close(g.checked)
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPostgresKnowledgeGapReadyCheckLocksUntilCommit(t *testing.T) {
	f := newGapPatchFixture(t)
	doc := f.upload(t, f.mentor)
	gate := gatedGapDocuments{uploads: knowledge.Uploads{DB: f.db}, checked: make(chan struct{}), release: make(chan struct{})}
	defer close(gate.release)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	reply := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		reply <- gapRequest(ctx, requestHandler(f.db, Dependencies{QuestionDocuments: gate}), f.path, gapResolution(1, "Готово", doc), f.mentor)
	}()
	select {
	case <-gate.checked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	revoked := make(chan error, 1)
	go func() {
		_, err := f.db.ExecContext(ctx, `UPDATE knowledge_documents SET users='["mentor_demo_1"]'::jsonb WHERE id=$1`, doc)
		revoked <- err
	}()
	waitGapLocks(t, f.db, "UPDATE knowledge_documents SET users=%", 1)
	// Allow the transaction to finish only after proving ACL revocation is blocked.
	select {
	case gate.release <- struct{}{}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case r := <-reply:
		if r.Code != 200 {
			t.Fatalf("PATCH %d: %s", r.Code, r.Body.String())
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-revoked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	saved := gapSnapshot(t, f.db, f.question.ID)
	apiRequest(t, f.handler, "PATCH", f.path, gapResolution(2, "Новое решение после отзыва", doc), f.mentor, 409)
	assertGapUnchanged(t, f, saved)
}
