package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func prepareQuestionContext(t *testing.T, direction string) (resolutionFixture, string, string) {
	t.Helper()
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	var message, user string
	if err := db.QueryRow(`UPDATE chat_messages SET routing=jsonb_build_object('direction',$2::text,'reason','Проверенный адресат','clarification',NULL,'split',false),outcome='knowledge_gap',text='Ответ из исходной инструкции' WHERE conversation_id=$1 AND role='assistant' RETURNING id,in_reply_to_message_id`, conversation, direction).Scan(&message, &user); err != nil {
		t.Fatal(err)
	}
	service := &knowledge.Service{DB: db, Embedder: uploadEmbedder{}}
	h := requestHandler(db, Dependencies{Knowledge: service})
	owner, other := "mentor_demo_1", "mentor_demo_2"
	if direction == "hr" {
		owner, other = "hr_demo_1", "hr_demo_2"
	}
	f := resolutionFixture{db: db, h: h, student: student, owner: loginCookie(t, h, owner, nil), other: loginCookie(t, h, other, nil), foreign: loginCookie(t, h, "intern_demo_other", nil), service: service, direction: direction, question: knowledgegaps.Question{Status: knowledgegaps.Status{MessageID: message}}}
	docID := "context-demo-doc"
	if direction == "hr" {
		doc := hrResponse[knowledge.HRDocument](t, h, "POST", "/hr/documents", map[string]any{"operation_id": hrKey(5701), "title": "Порядок оформления", "content": "# Кадровый порядок\nПодайте заявление.", "team_ids": []string{"team_demo_backend"}}, f.owner, 200)
		docID = doc.ID
		hrResponse[knowledge.HRDocument](t, h, "POST", "/hr/documents/"+docID+"/publish", map[string]any{"version": doc.Version}, f.owner, 200)
		if _, err := (knowledge.UploadWorker{Knowledge: service}).RunOne(t.Context()); err != nil {
			t.Fatal(err)
		}
	} else if err := service.Import(t.Context(), knowledge.Document{ID: docID, Source: "demo", SourceID: docID, Title: "Инструкция с приватным названием", Content: "# Доступ\nПодайте заявку.", TeamID: "team_demo_backend", Users: []string{}, Direction: "mentor", Version: 1, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	sources, _ := json.Marshal([]map[string]any{{"document_id": docID, "version": 1, "title": "Название не должно копироваться из сообщения", "excerpt": "Закрываемый текст"}})
	if _, err := db.Exec(`UPDATE chat_messages SET sources=$2 WHERE id=$1`, message, string(sources)); err != nil {
		t.Fatal(err)
	}
	return f, user, docID
}
func contextPath(f resolutionFixture) string { return "/messages/" + f.question.MessageID }
func contextInput(t *testing.T, f resolutionFixture, user, doc string) *knowledgegaps.ContextInput {
	var revision int64
	if err := f.db.QueryRow(`SELECT revision FROM onboarding_plans WHERE user_id=$1`, testkit.UserID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return &knowledgegaps.ContextInput{Reviewed: true, IncludeStep: true, StepRevision: &revision, Attempts: "Сам проверил настройки", Fragments: []knowledgegaps.ContextFragmentInput{{MessageID: user, Text: "Моя проверенная формулировка"}}, Documents: []knowledgegaps.ContextDocumentInput{{ID: doc, Version: 1}}}
}
func createContextQuestion(t *testing.T, f resolutionFixture, in *knowledgegaps.ContextInput, key int) knowledgegaps.Question {
	t.Helper()
	return hrResponse[knowledgegaps.Question](t, f.h, "POST", contextPath(f)+"/questions", knowledgegaps.Operation{ID: hrKey(key), Draft: knowledgegaps.Draft{Direction: f.direction, Question: "Уточните порядок", Context: "Проверенное краткое резюме", ContextSnapshot: in}}, f.student, 200)
}
func contextQuestionPath(f resolutionFixture, id string) string {
	return "/" + f.direction + "/questions/" + id
}

func TestPostgresQuestionContextLifecycle(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f, user, doc := prepareQuestionContext(t, direction)
			path := contextPath(f)
			preview := hrResponse[knowledgegaps.ContextSnapshot](t, f.h, "GET", path+"/question-context?direction="+direction, nil, f.student, 200)
			if len(preview.Fragments) != 2 || preview.Step == nil || len(preview.Documents) != 1 || preview.Documents[0].Opened {
				t.Fatal(preview)
			}
			for range 2 {
				hrResponse[any](t, f.h, "GET", path+"/context-documents/"+doc, nil, f.student, 200)
			}
			var count int
			if err := f.db.QueryRow(`SELECT count(*) FROM question_document_opens`).Scan(&count); err != nil || count != 0 {
				t.Fatal("A read without a render acknowledgement was counted", count, err)
			}
			for range 2 {
				hrResponse[any](t, f.h, "POST", path+"/context-documents/"+doc+"/opened", map[string]any{"version": 1}, f.student, 200)
			}
			if err := f.db.QueryRow(`SELECT count(*) FROM question_document_opens`).Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
			preview = hrResponse[knowledgegaps.ContextSnapshot](t, f.h, "GET", path+"/question-context?direction="+direction, nil, f.student, 200)
			if !preview.Documents[0].Opened {
				t.Fatal(preview)
			}
			in := contextInput(t, f, user, doc)
			in.Reviewed = false
			draft := knowledgegaps.Draft{Direction: direction, Question: "Сохранённый вопрос", Context: "Сохранённое резюме", ContextSnapshot: in}
			hrResponse[knowledgegaps.Draft](t, f.h, "PUT", path+"/question-drafts/"+direction, draft, f.student, 200)
			state := hrResponse[knowledgegaps.MessageQuestions](t, f.h, "GET", path+"/questions", nil, f.student, 200)
			if len(state.Drafts) != 1 || state.Drafts[0].ContextSnapshot.Attempts != in.Attempts {
				t.Fatal(state)
			}
			in.Reviewed = true
			q := createContextQuestion(t, f, in, 5702)
			queue := hrResponse[struct {
				Items []knowledgegaps.Question `json:"items"`
			}](t, f.h, "GET", "/"+direction+"/questions", nil, f.owner, 200)
			if len(queue.Items) != 1 || queue.Items[0].ContextSnapshot == nil {
				t.Fatal(queue)
			}
			if q.ContextSnapshot == nil || len(q.ContextSnapshot.Fragments) != 1 || !q.ContextSnapshot.Fragments[0].Edited || q.ContextSnapshot.Step == nil {
				t.Fatal(q)
			}
			view := hrResponse[knowledgegaps.Question](t, f.h, "GET", contextQuestionPath(f, q.ID), nil, f.owner, 200)
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), "Ответ из исходной инструкции") || !view.ContextSnapshot.Documents[0].Opened {
				t.Fatal(string(raw))
			}
			hrResponse[any](t, f.h, "GET", contextQuestionPath(f, q.ID)+"/context-documents/"+doc, nil, f.owner, 200)
			hrResponse[any](t, f.h, "GET", contextQuestionPath(f, q.ID)+"/context-documents/absent", nil, f.owner, 404)
			hrResponse[any](t, f.h, "GET", path+"/question-context?direction="+direction, nil, f.foreign, 404)
			hrResponse[any](t, f.h, "GET", path+"/context-documents/"+doc, nil, f.owner, 403)
			hrResponse[any](t, f.h, "GET", path+"/context-documents/absent", nil, f.student, 404)
			// Reconstructing the handler does not reconstruct or duplicate the snapshot.
			f.h = requestHandler(f.db, Dependencies{Knowledge: f.service})
			q = hrResponse[knowledgegaps.Question](t, f.h, "GET", contextQuestionPath(f, q.ID), nil, f.owner, 200)
			history := hrResponse[knowledgegaps.History](t, f.h, "GET", "/questions/"+q.ID, nil, f.student, 200)
			if history.Question.ContextSnapshot.Attempts != in.Attempts {
				t.Fatal(history)
			}
			state = hrResponse[knowledgegaps.MessageQuestions](t, f.h, "GET", path+"/questions", nil, f.student, 200)
			if len(state.Drafts) != 0 {
				t.Fatal(state)
			}
			other := "hr"
			if direction == "hr" {
				other = "mentor"
			}
			empty := &knowledgegaps.ContextInput{Reviewed: true, Fragments: []knowledgegaps.ContextFragmentInput{}, Documents: []knowledgegaps.ContextDocumentInput{}}
			moved := hrResponse[knowledgegaps.Question](t, f.h, "POST", "/questions/"+q.ID+"/reroute", knowledgegaps.Operation{ID: hrKey(5703), Revision: q.Revision, Draft: knowledgegaps.Draft{Direction: other, Question: "Отдельная тема", Context: "Новый подтверждённый текст", ContextSnapshot: empty}}, f.student, 200)
			if moved.StepID != nil || len(moved.ContextSnapshot.Fragments) != 0 {
				t.Fatal(moved)
			}
			hrResponse[any](t, f.h, "GET", contextQuestionPath(f, q.ID), nil, f.owner, 404)
			history = hrResponse[knowledgegaps.History](t, f.h, "GET", "/questions/"+q.ID, nil, f.student, 200)
			for _, entry := range history.Entries {
				if strings.Contains(string(entry.Snapshot), "context_snapshot") {
					t.Fatal(string(entry.Snapshot))
				}
			}
		})
	}
}

func TestPostgresQuestionContextRevocation(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f, user, doc := prepareQuestionContext(t, direction)
			in := contextInput(t, f, user, doc)
			in.Fragments = append(in.Fragments, knowledgegaps.ContextFragmentInput{MessageID: f.question.MessageID, Text: "Подтверждённый фрагмент ответа"})
			q := createContextQuestion(t, f, in, 5704)
			draft := knowledgegaps.Draft{Direction: direction, Question: "Черновик", ContextSnapshot: in}
			hrResponse[knowledgegaps.Draft](t, f.h, "PUT", contextPath(f)+"/question-drafts/"+direction, draft, f.student, 200)
			if direction == "hr" {
				hrResponse[any](t, f.h, "POST", "/hr/documents/"+doc+"/revoke", map[string]int{"version": 1}, f.owner, 200)
			} else if _, err := f.db.Exec(`DELETE FROM knowledge_documents WHERE id=$1`, doc); err != nil {
				t.Fatal(err)
			}
			got := hrResponse[knowledgegaps.Question](t, f.h, "GET", contextQuestionPath(f, q.ID), nil, f.owner, 200)
			if got.ContextSnapshot.Documents[0].Title != nil || got.ContextSnapshot.Documents[0].Status != "unavailable" || got.ContextSnapshot.Fragments[1].Text != nil {
				t.Fatal(got)
			}
			hrResponse[any](t, f.h, "GET", contextQuestionPath(f, q.ID)+"/context-documents/"+doc, nil, f.owner, 404)
			previews := hrResponse[knowledgegaps.ContextSnapshot](t, f.h, "GET", contextPath(f)+"/question-context?direction="+direction, nil, f.student, 200)
			if len(previews.Documents) != 0 || len(previews.Fragments) != 1 {
				t.Fatal(previews)
			}
			saved := hrResponse[knowledgegaps.MessageQuestions](t, f.h, "GET", contextPath(f)+"/questions", nil, f.student, 200)
			if saved.Drafts[0].ContextSnapshot.Fragments[1].Text != "" {
				t.Fatal(saved)
			}
			// A fresh operation cannot save an unavailable reference.
			hrResponse[any](t, f.h, "PUT", contextPath(f)+"/question-drafts/"+direction, draft, f.student, 409)
			hrResponse[any](t, f.h, "POST", "/questions/"+q.ID+"/reroute", knowledgegaps.Operation{ID: hrKey(5705), Revision: q.Revision, Draft: knowledgegaps.Draft{Direction: map[string]string{"hr": "mentor", "mentor": "hr"}[direction], Question: "Другая тема", ContextSnapshot: in}}, f.student, 409)
		})
	}
}

func TestPostgresQuestionContextConcurrentConfirmationAndValidation(t *testing.T) {
	f, user, doc := prepareQuestionContext(t, "mentor")
	in := contextInput(t, f, user, doc)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { createContextQuestion(t, f, in, 5706) })
	}
	wg.Wait()
	bad := *in
	bad.Attempts = "Другой текст"
	hrResponse[any](t, f.h, "POST", contextPath(f)+"/questions", knowledgegaps.Operation{ID: hrKey(5706), Draft: knowledgegaps.Draft{Direction: f.direction, Question: "Уточните порядок", Context: "Проверенное краткое резюме", ContextSnapshot: &bad}}, f.student, 409)
	for _, change := range []func(*knowledgegaps.ContextInput){
		func(c *knowledgegaps.ContextInput) { c.Attempts = strings.Repeat("я", 4001) },
		func(c *knowledgegaps.ContextInput) { c.Fragments = append(c.Fragments, c.Fragments[0]) }, func(c *knowledgegaps.ContextInput) { c.Fragments[0].Text = strings.Repeat("a", 801) },
		func(c *knowledgegaps.ContextInput) { c.Documents = append(c.Documents, c.Documents[0]) }, func(c *knowledgegaps.ContextInput) { c.Documents[0].Version = 0 },
	} {
		c := contextInput(t, f, user, doc)
		change(c)
		hrResponse[any](t, f.h, "PUT", contextPath(f)+"/question-drafts/mentor", knowledgegaps.Draft{Direction: "mentor", Question: "Вопрос", ContextSnapshot: c}, f.student, 422)
	}
	hrResponse[any](t, f.h, "GET", contextPath(f)+"/question-context?direction=other", nil, f.student, 422)
	hrResponse[any](t, f.h, "GET", contextPath(f)+"/question-context?direction=mentor", nil, f.owner, 403)
	hrResponse[any](t, f.h, "GET", "/messages/missing/question-context?direction=mentor", nil, f.student, 404)
	// Missing confirmation is rejected on send, but a non-reviewed draft is valid.
	c := contextInput(t, f, user, doc)
	c.Reviewed = false
	hrResponse[any](t, f.h, "PUT", contextPath(f)+"/question-drafts/mentor", knowledgegaps.Draft{Direction: "mentor", Question: "Вопрос", ContextSnapshot: c}, f.student, 200)
	hrResponse[any](t, f.h, "POST", contextPath(f)+"/questions", knowledgegaps.Operation{ID: hrKey(5707), Draft: knowledgegaps.Draft{Direction: "mentor", Question: "Вопрос", ContextSnapshot: c}}, f.student, 422)
}

func TestPostgresQuestionContextEmptyAndUnavailableDestinations(t *testing.T) {
	f, user, doc := prepareQuestionContext(t, "hr")
	in := contextInput(t, f, user, doc)
	if _, err := f.db.Exec(`UPDATE hr_team_assignments SET active=false WHERE team_id='team_demo_backend'`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "POST", contextPath(f)+"/questions", knowledgegaps.Operation{ID: hrKey(5708), Draft: knowledgegaps.Draft{Direction: "hr", Question: "Сохранить до назначения HR", ContextSnapshot: in}}, f.student, 409)
	state := hrResponse[knowledgegaps.MessageQuestions](t, f.h, "GET", contextPath(f)+"/questions", nil, f.student, 200)
	if len(state.Drafts) != 1 {
		t.Fatal(state)
	}
	// Manual mode works without eligible history or a checkpoint.
	if _, err := f.db.Exec(`UPDATE conversations SET step_id=NULL`); err != nil {
		t.Fatal(err)
	}
	preview := hrResponse[knowledgegaps.ContextSnapshot](t, f.h, "GET", contextPath(f)+"/question-context?direction=mentor", nil, f.student, 200)
	if len(preview.Fragments) != 0 || preview.Step != nil {
		t.Fatal(preview)
	}
	in = &knowledgegaps.ContextInput{Reviewed: true, Attempts: "Заполнил вручную", Fragments: []knowledgegaps.ContextFragmentInput{}, Documents: []knowledgegaps.ContextDocumentInput{}}
	f.direction = "mentor"
	q := createContextQuestion(t, f, in, 5709)
	if q.StepID != nil || q.ContextSnapshot.Attempts != in.Attempts {
		t.Fatal(q)
	}
}

func TestQuestionContextUnauthenticated(t *testing.T) {
	s := knowledgegaps.Store{}
	if _, err := s.ContextPreview(context.Background(), "x", "mentor"); err != identity.ErrUnauthenticated {
		t.Fatal(err)
	}
	if _, err := s.ContextDocument(context.Background(), "x", "y"); err != identity.ErrUnauthenticated {
		t.Fatal(err)
	}
	if _, err := s.OpenContextDocument(context.Background(), "x", "y"); err != identity.ErrUnauthenticated {
		t.Fatal(err)
	}
	if err := s.RecordContextOpen(context.Background(), "x", "y", 1); err != identity.ErrUnauthenticated {
		t.Fatal(err)
	}
}

func TestPostgresQuestionContextStaleVersionsAndAssignment(t *testing.T) {
	f, user, doc := prepareQuestionContext(t, "mentor")
	in := contextInput(t, f, user, doc)
	if _, err := f.db.Exec(`UPDATE onboarding_plans SET revision=revision+1 WHERE user_id=$1`, testkit.UserID); err != nil {
		t.Fatal(err)
	}
	body := knowledgegaps.Operation{ID: hrKey(5710), Draft: knowledgegaps.Draft{Direction: "mentor", Question: "Проверить версии", ContextSnapshot: in}}
	hrResponse[any](t, f.h, "POST", contextPath(f)+"/questions", body, f.student, 409)
	*in.StepRevision++
	if _, err := f.db.Exec(`UPDATE knowledge_documents SET version=2,title='Свежая инструкция' WHERE id=$1`, doc); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "POST", contextPath(f)+"/questions", body, f.student, 409)
	in.Documents[0].Version = 2
	q := hrResponse[knowledgegaps.Question](t, f.h, "POST", contextPath(f)+"/questions", body, f.student, 200)
	if q.ContextSnapshot.Documents[0].Version != 2 {
		t.Fatal(q)
	}
	if _, err := f.db.Exec(`UPDATE knowledge_documents SET users='["intern_demo_1","mentor_demo_1"]' WHERE id=$1`, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE mentor_assignments SET mentor_id='mentor_demo_2' WHERE intern_id=$1`, testkit.UserID); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "GET", contextQuestionPath(f, q.ID), nil, f.owner, 404)
	read := hrResponse[knowledgegaps.Question](t, f.h, "GET", contextQuestionPath(f, q.ID), nil, f.other, 200)
	if read.ContextSnapshot.Documents[0].Title != nil {
		t.Fatal("recipient received a document outside its ACL", read)
	}
	hrResponse[any](t, f.h, "GET", contextQuestionPath(f, q.ID)+"/context-documents/"+doc, nil, f.other, 404)
	hrResponse[any](t, f.h, "GET", "/questions/"+q.ID+"/context-documents/"+doc, nil, f.student, 200)
}

func TestPostgresQuestionContextOpeningAcknowledgement(t *testing.T) {
	f, user, doc := prepareQuestionContext(t, "hr")
	q := createContextQuestion(t, f, contextInput(t, f, user, doc), 5711)
	path := contextPath(f) + "/context-documents/" + doc + "/opened"
	for _, bad := range []any{map[string]any{"version": 0}, map[string]any{"version": "invalid"}, map[string]any{"opened": true}} {
		hrResponse[any](t, f.h, "POST", path, bad, f.student, 422)
	}
	hrResponse[any](t, f.h, "POST", path, map[string]any{"version": 2}, f.student, 409)
	hrResponse[any](t, f.h, "POST", path, map[string]any{"version": 1}, f.foreign, 404)
	hrResponse[any](t, f.h, "POST", path, map[string]any{"version": 1}, f.owner, 403)
	hrResponse[any](t, f.h, "GET", "/mentor/questions/"+q.ID+"/context-documents/"+doc, nil, f.owner, 403)
	hrResponse[any](t, f.h, "POST", path, map[string]any{"version": 1}, f.student, 200)
	read := hrResponse[knowledgegaps.Question](t, f.h, "GET", contextQuestionPath(f, q.ID), nil, f.owner, 200)
	if read.ContextSnapshot.Documents[0].Opened {
		t.Fatal("a later opening changed the sent snapshot")
	}
	if _, err := f.db.Exec(`UPDATE hr_team_assignments SET active=false WHERE hr_user_id='hr_demo_1' AND team_id='team_demo_backend'`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "GET", contextQuestionPath(f, q.ID)+"/context-documents/"+doc, nil, f.owner, 404)
}

func TestPostgresQuestionContextRejectsForgedReferences(t *testing.T) {
	f, user, doc := prepareQuestionContext(t, "mentor")
	in := contextInput(t, f, user, doc)
	in.Fragments[0].MessageID = "absent-message"
	hrResponse[any](t, f.h, "PUT", contextPath(f)+"/question-drafts/mentor", knowledgegaps.Draft{Direction: "mentor", Question: "Чужая ссылка", ContextSnapshot: in}, f.student, 409)
	in = contextInput(t, f, user, doc)
	in.IncludeStep = false
	in.StepRevision = nil
	in.Documents[0].ID = "absent-document"
	hrResponse[any](t, f.h, "POST", contextPath(f)+"/questions", knowledgegaps.Operation{ID: hrKey(5712), Draft: knowledgegaps.Draft{Direction: "mentor", Question: "Не предлагаемый источник", ContextSnapshot: in}}, f.student, 409)
	in = contextInput(t, f, user, doc)
	raw, _ := json.Marshal(in)
	var forged map[string]any
	_ = json.Unmarshal(raw, &forged)
	forged["step_id"] = "foreign-step"
	hrResponse[any](t, f.h, "POST", contextPath(f)+"/questions", map[string]any{"operation_id": hrKey(5712), "direction": "mentor", "question": "Подмена", "context_excerpt": "", "context_snapshot": forged}, f.student, 422)
}
