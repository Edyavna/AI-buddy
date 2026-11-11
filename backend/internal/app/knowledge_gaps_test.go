package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type readyQuestionDocument struct{}

func (readyQuestionDocument) CheckReady(_ context.Context, _ *sql.Tx, mentor, intern identity.Principal, ids []string) error {
	if mentor.UserID == "mentor_demo_1" && intern.UserID == testkit.UserID && len(ids) == 1 && ids[0] == "ready" {
		return nil
	}
	return ports.ErrNotFound
}

func TestPostgresKnowledgeGapLifecycle(t *testing.T) {
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	var assistant string
	if err := db.QueryRow("SELECT id FROM chat_messages WHERE conversation_id=$1 AND role='assistant'", conversation).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{QuestionDocuments: readyQuestionDocument{}})
	path := "/api/v1/messages/" + assistant + "/knowledge-gap"
	if body := apiRequest(t, h, "GET", path, "", student, 200).Body.String(); body != "{\"question\":null}\n" {
		t.Fatal(body)
	}
	apiRequest(t, h, "POST", path, "", student, 409)
	if _, err := db.Exec("UPDATE chat_messages SET outcome='knowledge_gap' WHERE id=$1", assistant); err != nil {
		t.Fatal(err)
	}

	store := knowledgegaps.Store{DB: db}
	ctx := testkit.Context(context.Background())
	var wg sync.WaitGroup
	creations := make(chan bool, 2)
	for range 2 {
		wg.Go(func() {
			_, created, err := store.Intern(ctx, assistant, true)
			if err != nil {
				t.Error(err)
				return
			}
			creations <- created
		})
	}
	wg.Wait()
	close(creations)
	count := 0
	for created := range creations {
		if created {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("creations = %d", count)
	}
	reply := apiRequest(t, h, "POST", path, "", student, 200)
	var state knowledgegaps.Status
	if err := json.Unmarshal(reply.Body.Bytes(), &state); err != nil || state.ID == "" || state.State != "new" {
		t.Fatal(err, reply.Body.String())
	}
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	other := loginCookie(t, h, "mentor_demo_2", nil)
	question := "/api/v1/mentor/questions/" + state.ID
	apiRequest(t, h, "GET", question, "", other, 404)
	apiRequest(t, h, "GET", question, "", student, 403)
	apiRequest(t, h, "PATCH", question, `{"status":"in_progress","revision":1}`, mentor, 200)
	apiRequest(t, h, "PATCH", question, `{"status":"resolved","revision":1,"resolution":"Инструкция","document_ids":["ready"]}`, mentor, 409)
	apiRequest(t, h, "PATCH", question, `{"status":"resolved","revision":2,"resolution":"Инструкция","document_ids":["foreign"]}`, mentor, 409)
	apiRequest(t, h, "PATCH", question, `{"status":"resolved","revision":2,"resolution":"Инструкция","document_ids":["ready"]}`, mentor, 200)
	h = requestHandler(db, Dependencies{QuestionDocuments: readyQuestionDocument{}})
	reply = apiRequest(t, h, "GET", path, "", student, 200)
	var status struct {
		Question knowledgegaps.Status `json:"question"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &status); err != nil || status.Question.State != "resolved" || status.Question.Revision != 3 {
		t.Fatal(err, reply.Body.String())
	}
	apiRequest(t, h, "PATCH", question, `{"status":"in_progress","revision":3}`, mentor, 200)
	var history int
	if err := db.QueryRow("SELECT count(*) FROM knowledge_gap_history WHERE question_id=$1", state.ID).Scan(&history); err != nil || history != 3 {
		t.Fatal(err, history)
	}
}
