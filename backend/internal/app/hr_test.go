package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func hrResponse[T any](t *testing.T, h http.Handler, method, path string, body any, cookie *http.Cookie, status int) T {
	t.Helper()
	input := ""
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = string(raw)
	}
	rec := apiRequest(t, h, method, "/api/v1"+path, input, cookie, status)
	var out T
	if status == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err, rec.Body.String())
		}
	}
	return out
}

func hrKey(number int) string { return fmt.Sprintf("87654321-1234-4321-9876-%012d", number) }

func TestPostgresHRLifecycle(t *testing.T) {
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	var assistant string
	if err := db.QueryRow(`UPDATE chat_messages SET routing='{"direction":"hr","reason":"Приватная тема отпуска","clarification":null,"split":false}' WHERE conversation_id=$1 AND role='assistant' RETURNING id`, conversation).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	service := &knowledge.Service{DB: db, Embedder: uploadEmbedder{}}
	h := requestHandler(db, Dependencies{Knowledge: service})
	hr1 := loginCookie(t, h, "hr_demo_1", nil)
	hr2 := loginCookie(t, h, "hr_demo_2", nil)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	scope := hrResponse[hrscope.Scope](t, h, "GET", "/me/access-scope", nil, hr1, 200)
	if len(scope.Teams) != 2 {
		t.Fatal(scope)
	}
	hrResponse[any](t, h, "GET", "/hr/questions?team_id=team_demo_frontend", nil, hr2, 404)
	hrResponse[any](t, h, "GET", "/hr/questions", nil, mentor, 403)
	hrResponse[any](t, h, "PUT", "/onboarding/steps/step_local_setup/confirmation", map[string]any{"confirmed": true, "revision": 1}, hr1, 403)
	path := "/messages/" + assistant + "/questions"
	other := loginCookie(t, h, "intern_demo_other", nil)
	hrResponse[any](t, h, "GET", path, nil, other, 404)
	create := knowledgegaps.Operation{ID: hrKey(1), Draft: knowledgegaps.Draft{Direction: "hr", Question: "Как оформить отпуск?", Context: "Согласованный контекст"}}
	q := hrResponse[knowledgegaps.Question](t, h, "POST", path, create, student, 200)
	if q.Direction != "hr" || q.SpecialistID != nil || q.Revision != 1 {
		t.Fatal(q)
	}
	if replay := hrResponse[knowledgegaps.Question](t, h, "POST", path, create, student, 200); replay.ID != q.ID {
		t.Fatal(replay)
	}
	create.ID = hrKey(2)
	create.Direction = "mentor"
	create.Question = "Как открыть pull request?"
	work := hrResponse[knowledgegaps.Question](t, h, "POST", path, create, student, 200)
	hrResponse[any](t, h, "GET", "/mentor/questions/"+q.ID, nil, mentor, 404)
	hrResponse[any](t, h, "GET", "/hr/questions/"+work.ID, nil, hr1, 404)
	list := hrResponse[struct{ Items []knowledgegaps.Question }](t, h, "GET", "/hr/questions?status=new&q=отпуск", nil, hr1, 200)
	if len(list.Items) != 1 {
		t.Fatal(list)
	}
	store := knowledgegaps.Store{DB: db, Documents: knowledge.Uploads{DB: db}, Knowledge: service}
	var wg sync.WaitGroup
	result := make(chan knowledgegaps.Question, 2)
	for i, id := range []string{"hr_demo_1", "hr_demo_2"} {
		wg.Go(func() {
			ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: id, Role: identity.HR, TeamID: "team_demo_backend"})
			claimed, err := store.HRPatch(ctx, q.ID, knowledgegaps.HRUpdate{ID: hrKey(3 + i), Action: "claim", Revision: 1})
			if err == nil {
				result <- claimed
			}
		})
	}
	wg.Wait()
	close(result)
	if len(result) != 1 {
		t.Fatalf("claims = %d", len(result))
	}
	claimed := <-result
	owner := hr1
	if *claimed.SpecialistID == "hr_demo_2" {
		owner = hr2
	}
	docInput := knowledge.HRDocumentInput{Operation: hrKey(10), Title: "Порядок отпуска", Content: "# Отпуск\nЗаявление подаётся заранее.", Teams: []string{"team_demo_backend"}}
	doc := hrResponse[knowledge.HRDocument](t, h, "POST", "/hr/documents", docInput, hr1, 200)
	docPath := "/hr/documents/" + doc.ID
	readPath := "/documents/" + doc.ID
	hrResponse[any](t, h, "GET", readPath, nil, student, 404)
	hrResponse[any](t, h, "POST", docPath+"/publish", map[string]any{"version": 1}, hr2, 200)
	failed := knowledge.UploadWorker{Knowledge: &knowledge.Service{DB: db, Embedder: uploadEmbedder{fail: true}}}
	if processed, err := failed.RunOne(t.Context()); !processed || err != nil {
		t.Fatal(processed, err)
	}
	hrResponse[any](t, h, "POST", docPath+"/retry", map[string]any{"version": 1}, hr2, 200)
	worker := knowledge.UploadWorker{Knowledge: service}
	if processed, err := worker.RunOne(t.Context()); !processed || err != nil {
		t.Fatal(processed, err)
	}
	hrResponse[any](t, h, "GET", readPath, nil, student, 200)
	hrResponse[any](t, h, "GET", readPath, nil, other, 404)
	hrResponse[any](t, h, "GET", readPath, nil, mentor, 404)
	qPath := "/hr/questions/" + q.ID
	update := knowledgegaps.HRUpdate{ID: hrKey(12), Revision: claimed.Revision, Action: "answer", Resolution: "Следуйте инструкции", Documents: []string{doc.ID}}
	answered := hrResponse[knowledgegaps.Question](t, h, "PATCH", qPath, update, owner, 200)
	update.ID, update.Revision, update.Action = hrKey(13), answered.Revision, "resolve"
	resolved := hrResponse[knowledgegaps.Question](t, h, "PATCH", qPath, update, owner, 200)
	if resolved.State != "resolved" {
		t.Fatal(resolved)
	}
	h = requestHandler(db, Dependencies{Knowledge: service})
	history := hrResponse[knowledgegaps.History](t, h, "GET", "/questions/"+q.ID, nil, student, 200)
	if len(history.Question.DocumentIDs) != 1 || len(history.Entries) != 3 {
		t.Fatal(history)
	}
	docInput.Operation, docInput.Version, docInput.Content = "", 1, "Обновлённая инструкция"
	edit := hrResponse[knowledge.HRDocument](t, h, "PATCH", docPath, docInput, hr2, 200)
	if edit.Version != 2 || edit.Published == nil || *edit.Published != 1 {
		t.Fatal(edit)
	}
	hrResponse[any](t, h, "PATCH", docPath, docInput, hr1, 409)
	hrResponse[any](t, h, "POST", docPath+"/publish", map[string]any{"version": 2}, hr2, 200)
	if processed, err := worker.RunOne(t.Context()); !processed || err != nil {
		t.Fatal(processed, err)
	}
	if current := hrResponse[map[string]any](t, h, "GET", readPath, nil, student, 200); current["content"] != docInput.Content {
		t.Fatal(current)
	}
	hrResponse[any](t, h, "POST", docPath+"/revoke", map[string]any{"version": 2}, hr2, 200)
	hrResponse[any](t, h, "GET", readPath, nil, student, 404)
	if history := hrResponse[knowledgegaps.History](t, h, "GET", "/questions/"+q.ID, nil, student, 200); len(history.Question.DocumentIDs) != 0 {
		t.Fatal(history)
	}
	reroute := knowledgegaps.Operation{ID: hrKey(20), Revision: resolved.Revision, Draft: knowledgegaps.Draft{Direction: "mentor", Question: "Как оформить код-ревью?"}}
	newQ := hrResponse[knowledgegaps.Question](t, h, "POST", "/questions/"+q.ID+"/reroute", reroute, student, 200)
	if newQ.ID != q.ID || newQ.Resolution != nil || newQ.State != "new" {
		t.Fatal(newQ)
	}
	hrResponse[any](t, h, "POST", "/questions/"+q.ID+"/reroute", reroute, student, 200)
	hrResponse[any](t, h, "GET", qPath, nil, owner, 404)
	hrResponse[any](t, h, "GET", "/mentor/questions/"+q.ID, nil, mentor, 200)

	assignments := hrscope.Store{DB: db}
	if err := assignments.Set(t.Context(), "hr_demo_1", "team_demo_backend", false); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	now := hrResponse[hrscope.Scope](t, h, "GET", "/me/access-scope", nil, hr1, 200)
	if len(now.Teams) != 1 || now.Version == scope.Version {
		t.Fatal(now)
	}
	hrResponse[any](t, h, "GET", docPath, nil, hr1, 404)
	hrResponse[any](t, h, "GET", "/hr/documents", nil, hr2, 200)
	hrResponse[any](t, h, "GET", docPath, nil, hr2, 200)
	hrResponse[any](t, h, "GET", "/hr/documents", nil, mentor, 403)
	hrResponse[any](t, h, "GET", "/hr/documents/not-a-uuid", nil, hr2, 404)
	hrResponse[any](t, h, "POST", "/hr/documents", map[string]any{"title": "Без аудитории"}, hr2, 422)
	hrResponse[any](t, h, "POST", docPath+"/invalid", map[string]any{"version": 2}, hr2, 422)
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{UserID: "intern_demo_1", Role: identity.Intern, TeamID: "team_demo_backend"})
	if hits, err := service.SearchDirection(ctx, "Отпуск", "hr"); err != nil || len(hits) != 0 {
		t.Fatal(hits, err)
	}
}

func TestPostgresHRDraftWithoutRecipient(t *testing.T) {
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	var assistant string
	if err := db.QueryRow("SELECT id FROM chat_messages WHERE conversation_id=$1 AND role='assistant'", conversation).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	scope := hrscope.Store{DB: db}
	for _, hr := range []string{"hr_demo_1", "hr_demo_2"} {
		if err := scope.Set(t.Context(), hr, "team_demo_backend", false); err != nil {
			t.Fatal(err)
		}
	}
	h := requestHandler(db, Dependencies{})
	path := "/messages/" + assistant
	draft := map[string]string{"question": "Когда выходить?", "context_excerpt": "Согласовано"}
	hrResponse[any](t, h, "PUT", path+"/question-drafts/hr", draft, student, 200)
	op := knowledgegaps.Operation{ID: hrKey(1), Draft: knowledgegaps.Draft{Direction: "hr", Question: "Отпуск", Context: "Изменённый текст"}}
	hrResponse[any](t, h, "POST", path+"/questions", op, student, 409)
	result := hrResponse[knowledgegaps.MessageQuestions](t, h, "GET", path+"/questions", nil, student, 200)
	if len(result.Items) != 0 || len(result.Drafts) != 1 || !strings.Contains(result.Drafts[0].Context, "Изменённый") {
		t.Fatal(result)
	}
	if err := scope.Set(t.Context(), "hr_demo_1", "team_demo_backend", true); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, h, "POST", path+"/questions", op, student, 200)
}

func TestPostgresHRRevocationDuringIndexing(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	service := &knowledge.Service{DB: db, Embedder: uploadEmbedder{}}
	h := requestHandler(db, Dependencies{Knowledge: service})
	hr1 := loginCookie(t, h, "hr_demo_1", nil)
	hr2 := loginCookie(t, h, "hr_demo_2", nil)
	student := loginCookie(t, h, "intern_demo_other", nil)
	doc := hrResponse[knowledge.HRDocument](t, h, "POST", "/hr/documents", knowledge.HRDocumentInput{
		Operation: hrKey(23), Title: "Две команды", Content: "# Кадровая инструкция", Teams: []string{"team_demo_backend", "team_demo_frontend"}}, hr1, 200)
	path := "/hr/documents/" + doc.ID
	version := map[string]int{"version": 1}
	hrResponse[any](t, h, "GET", path, nil, hr2, 404)
	hrResponse[any](t, h, "POST", path+"/publish", version, hr2, 404)
	hrResponse[any](t, h, "POST", path+"/publish", version, hr1, 200)
	entered, release := make(chan struct{}), make(chan struct{})
	worker := knowledge.UploadWorker{Knowledge: &knowledge.Service{DB: db, Embedder: uploadEmbedder{entered: entered, release: release}}}
	done := make(chan error, 1)
	go func() { _, err := worker.RunOne(t.Context()); done <- err }()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	scope := hrscope.Store{DB: db}
	if err := scope.Set(t.Context(), "hr_demo_1", "team_demo_frontend", false); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, h, "GET", "/documents/"+doc.ID, nil, student, 404)
	if err := scope.Set(t.Context(), "hr_demo_1", "team_demo_frontend", true); err != nil {
		t.Fatal(err)
	}
	current := hrResponse[knowledge.HRDocument](t, h, "GET", path, nil, hr1, 200)
	if current.Status != "failed" || current.Published != nil {
		t.Fatal(current)
	}
	hrResponse[any](t, h, "POST", path+"/retry", version, hr1, 200)
	if processed, err := (knowledge.UploadWorker{Knowledge: service}).RunOne(t.Context()); !processed || err != nil {
		t.Fatal(processed, err)
	}
	hrResponse[any](t, h, "GET", "/documents/"+doc.ID, nil, student, 200)
}

func TestPostgresHRRevokedSpecialistReleasesClaimAndRerouteKeepsPrivateHistory(t *testing.T) {
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	h := requestHandler(db, Dependencies{})
	hr1 := loginCookie(t, h, "hr_demo_1", nil)
	hr2 := loginCookie(t, h, "hr_demo_2", nil)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	var assistant string
	if err := db.QueryRow("SELECT id FROM chat_messages WHERE conversation_id=$1 AND role='assistant'", conversation).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	path := "/messages/" + assistant + "/questions"
	op := knowledgegaps.Operation{ID: hrKey(1), Draft: knowledgegaps.Draft{Direction: "hr", Question: "Отпуск по семейной причине", Context: "Личный контекст"}}
	hrResponse[any](t, h, "POST", path, map[string]string{"direction": "hr", "team_id": "team_demo_frontend"}, student, 422)
	hrResponse[any](t, h, "POST", path, knowledgegaps.Operation{ID: "invalid", Draft: op.Draft}, student, 422)
	q := hrResponse[knowledgegaps.Question](t, h, "POST", path, op, student, 200)
	op.Question = "Другая версия"
	hrResponse[any](t, h, "POST", path, op, student, 409)
	hrPath := "/hr/questions/" + q.ID
	first := knowledgegaps.HRUpdate{ID: hrKey(2), Action: "claim", Revision: 1}
	q = hrResponse[knowledgegaps.Question](t, h, "PATCH", hrPath, first, hr1, 200)
	replayed := hrResponse[knowledgegaps.Question](t, h, "PATCH", hrPath, first, hr1, 200)
	if replayed.Revision != q.Revision || replayed.AppliedRevision == nil || *replayed.AppliedRevision != q.Revision {
		t.Fatal(replayed)
	}
	hrResponse[any](t, h, "PATCH", hrPath, knowledgegaps.HRUpdate{ID: hrKey(3), Action: "claim", Revision: 1}, hr2, 409)
	hrResponse[any](t, h, "PATCH", hrPath, knowledgegaps.HRUpdate{ID: hrKey(4), Action: "resolve", Revision: q.Revision}, hr1, 422)
	hrResponse[any](t, h, "PATCH", hrPath, knowledgegaps.HRUpdate{ID: hrKey(5), Action: "reopen", Revision: q.Revision}, hr1, 409)
	q = hrResponse[knowledgegaps.Question](t, h, "PATCH", hrPath, knowledgegaps.HRUpdate{ID: hrKey(6), Action: "answer", Revision: q.Revision, Resolution: "Приватный ответ HR"}, hr1, 200)
	if err := (hrscope.Store{DB: db}).Set(t.Context(), "hr_demo_1", "team_demo_backend", false); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, h, "GET", hrPath, nil, hr1, 404)
	hrResponse[any](t, h, "PATCH", hrPath, first, hr1, 404)
	q = hrResponse[knowledgegaps.Question](t, h, "GET", hrPath, nil, hr2, 200)
	if q.SpecialistID != nil || q.State != "new" {
		t.Fatal(q)
	}
	revision := q.Revision
	q = hrResponse[knowledgegaps.Question](t, h, "PATCH", hrPath, knowledgegaps.HRUpdate{ID: hrKey(7), Action: "claim", Revision: revision}, hr2, 200)
	transfer := knowledgegaps.Operation{ID: hrKey(8), Revision: revision, Draft: knowledgegaps.Draft{Direction: "mentor", Question: "Помогите с первым PR", Context: "Новый контекст"}}
	hrResponse[any](t, h, "POST", "/questions/"+q.ID+"/reroute", transfer, student, 409)
	transfer.Revision = q.Revision
	hrResponse[any](t, h, "POST", "/questions/"+q.ID+"/reroute", transfer, student, 200)
	body := apiRequest(t, h, "GET", "/api/v1/mentor/questions/"+q.ID, "", mentor, 200).Body.String()
	for _, secret := range []string{"Личный контекст", "Приватный ответ HR", "семейной причине", "Приватная тема отпуска"} {
		if strings.Contains(body, secret) {
			t.Fatal("prior route leaked:", body)
		}
	}
	if !strings.Contains(body, "Новый контекст") {
		t.Fatal(body)
	}
	hrResponse[any](t, h, "GET", hrPath, nil, hr2, 404)
}
