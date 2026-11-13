package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type resolutionFixture struct {
	db                             *sql.DB
	h                              http.Handler
	owner, student, other, foreign *http.Cookie
	question                       knowledgegaps.Question
	service                        *knowledge.Service
	path, direction                string
}

type resolutionHRModel struct{}

func (resolutionHRModel) Complete(context.Context, []agent.Message) (agent.Message, error) {
	return agent.Message{}, ports.ErrInvalid
}
func (resolutionHRModel) Classify(context.Context, string) (ports.RoutingSuggestion, error) {
	return ports.RoutingSuggestion{Direction: "hr", Reason: "Детерминированная проверка кадрового источника"}, nil
}
func (resolutionHRModel) AnswerHR(_ context.Context, _ string, docs []ports.Document) (string, string, error) {
	if len(docs) == 0 {
		return "", "", ports.ErrNotFound
	}
	return docs[0].Content, docs[0].ID, nil
}

func prepareResolution(t *testing.T, direction string) resolutionFixture {
	t.Helper()
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	var assistant string
	if err := db.QueryRow(`UPDATE chat_messages SET outcome='knowledge_gap' WHERE conversation_id=$1 AND role='assistant' RETURNING id`, conversation).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	service := &knowledge.Service{DB: db, Embedder: uploadEmbedder{}}
	h := requestHandler(db, Dependencies{Knowledge: service, ResolutionGenerator: knowledge.ResolutionGenerator{Demo: true}})
	ownerID, otherID := "mentor_demo_1", "mentor_demo_2"
	if direction == "hr" {
		ownerID, otherID = "hr_demo_1", "hr_demo_2"
	}
	f := resolutionFixture{db: db, h: h, owner: loginCookie(t, h, ownerID, nil), other: loginCookie(t, h, otherID, nil), student: student, foreign: loginCookie(t, h, "intern_demo_other", nil), service: service, direction: direction}
	f.question = hrResponse[knowledgegaps.Question](t, h, "POST", "/messages/"+assistant+"/questions", knowledgegaps.Operation{ID: hrKey(5601), Draft: knowledgegaps.Draft{Direction: direction, Question: "Как оформить инструкцию?", Context: "Одобренный контекст"}}, student, 200)
	qpath := "/" + direction + "/questions/" + f.question.ID
	if direction == "hr" {
		f.question = hrResponse[knowledgegaps.Question](t, h, "PATCH", qpath, knowledgegaps.HRUpdate{ID: hrKey(5602), Revision: 1, Action: "claim"}, f.owner, 200)
		f.question = hrResponse[knowledgegaps.Question](t, h, "PATCH", qpath, knowledgegaps.HRUpdate{ID: hrKey(5603), Revision: f.question.Revision, Action: "resolve", Resolution: "Заполните форму и согласуйте с руководителем."}, f.owner, 200)
	} else {
		rec := postMarkdown(t, h, f.owner, hrKey(5604), f.question.ID, "original.md", "# Исходная инструкция", 202)
		var doc knowledge.Upload
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if _, err := (knowledge.UploadWorker{Knowledge: service}).RunOne(t.Context()); err != nil {
			t.Fatal(err)
		}
		resolution := "Заполните форму и согласуйте с руководителем."
		ids := []string{doc.ID}
		f.question = hrResponse[knowledgegaps.Question](t, h, "PATCH", qpath, knowledgegaps.Update{State: "resolved", Revision: 1, Resolution: &resolution, DocumentIDs: &ids}, f.owner, 200)
	}
	f.path = qpath + "/instruction-draft"
	return f
}
func (f resolutionFixture) workspace(t *testing.T) knowledge.ResolutionWorkspace {
	return hrResponse[knowledge.ResolutionWorkspace](t, f.h, "GET", f.path, nil, f.owner, 200)
}
func (f resolutionFixture) save(t *testing.T, in knowledge.ResolutionInput) knowledge.ResolutionDraft {
	return hrResponse[knowledge.ResolutionDraft](t, f.h, "PUT", f.path, in, f.owner, 200)
}
func (f resolutionFixture) publish(t *testing.T, revision int64, action string) knowledge.ResolutionDraft {
	return hrResponse[knowledge.ResolutionDraft](t, f.h, "POST", f.path+"/"+action, map[string]any{"revision": revision, "reviewed": true}, f.owner, 200)
}

func TestPostgresResolutionLifecycle(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f := prepareResolution(t, direction)
			if w := f.workspace(t); w.Draft != nil || w.Source.Direction != direction || w.Source.Resolution == "" {
				t.Fatal(w)
			}
			hrResponse[any](t, f.h, "GET", f.path, nil, f.student, 403)
			hrResponse[any](t, f.h, "GET", f.path, nil, f.other, 404)
			hrResponse[any](t, f.h, "GET", f.path, nil, f.foreign, 403)
			generated := hrResponse[knowledge.ResolutionSuggestion](t, f.h, "POST", f.path+"/generate", nil, f.owner, 200)
			if generated.Mode != "demo" || generated.Content == "" {
				t.Fatal(generated)
			}
			in := knowledge.ResolutionInput{Title: "Общая инструкция", Content: "# Порядок\nЗаполните общую форму."}
			d := f.save(t, in)
			if d.Revision != 1 || d.Status != "draft" {
				t.Fatal(d)
			}
			// Restart the HTTP composition root: the draft is database-owned.
			f.h = requestHandler(f.db, Dependencies{Knowledge: f.service})
			if f.workspace(t).Draft.Content != in.Content {
				t.Fatal("lost draft")
			}
			hrResponse[any](t, f.h, "PUT", f.path, in, f.owner, 409)
			in.Revision = 1
			in.Content += "\nСогласуйте с руководителем."
			d = f.save(t, in)
			hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": 2, "reviewed": false}, f.owner, 422)
			d = f.publish(t, 2, "publish")
			if d.Status != "indexing" || d.DocumentID == nil {
				t.Fatal(d)
			}
			for range 2 {
				if replay := f.publish(t, 2, "publish"); *replay.DocumentID != *d.DocumentID {
					t.Fatal("duplicate")
				}
			}
			hrResponse[any](t, f.h, "GET", "/documents/"+*d.DocumentID, nil, f.student, 404)
			failed := knowledge.UploadWorker{Knowledge: &knowledge.Service{DB: f.db, Embedder: uploadEmbedder{fail: true}}}
			if _, err := failed.RunOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			if w := f.workspace(t); w.Draft.Status != "failed" || w.Draft.Error == nil {
				t.Fatal(w)
			}
			f.publish(t, 2, "retry")
			worker := knowledge.UploadWorker{Knowledge: f.service}
			if _, err := worker.RunOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			ready := f.workspace(t)
			if ready.Draft.Status != "ready" {
				t.Fatal(ready)
			}
			hrResponse[any](t, f.h, "GET", "/documents/"+*d.DocumentID, nil, f.student, 200)
			hrResponse[any](t, f.h, "GET", "/documents/"+*d.DocumentID, nil, f.foreign, 404)
			history := hrResponse[knowledgegaps.History](t, f.h, "GET", "/questions/"+f.question.ID, nil, f.student, 200)
			if !strings.Contains(fmt.Sprint(history.Question.DocumentIDs), *d.DocumentID) {
				t.Fatal(history)
			}
			ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "intern_demo_1", TeamID: "team_demo_backend", Role: identity.Intern})
			sources, err := f.service.Search(ctx, "Общая форма")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, source := range sources {
				if source.DocumentID == *d.DocumentID {
					found = true
				}
			}
			if !found {
				t.Fatal(sources)
			}
			var model agent.Model = &agent.Script{Replies: []agent.Message{
				{Calls: []agent.Call{{ID: "find", Type: "function", Function: agent.Function{Name: "search_knowledge", Arguments: `{"query":"Общая форма"}`}}}},
				{Calls: []agent.Call{{ID: "read", Type: "function", Function: agent.Function{Name: "read_document", Arguments: fmt.Sprintf(`{"document_id":%q}`, *d.DocumentID)}}}},
				{Content: `{"answer":"Заполните общую форму.","outcome":"answered"}`},
			}}
			a := agent.Agent{Model: model, Knowledge: f.service, Tasks: testkit.Tasks{}, Access: &testkit.Access{}, Requests: testkit.Requests{}, Drafts: testkit.Drafts(func(context.Context, ports.DraftInput) (ports.Draft, error) { return ports.Draft{}, ports.ErrInvalid })}
			if direction == "hr" {
				a.Model = resolutionHRModel{}
				a.Classifier = resolutionHRModel{}
			}
			answer, err := a.Run(ctx, ports.AgentInput{ConversationID: "resolution-conversation", AssistantMessageID: "resolution-answer", Text: "Как заполнить общую форму?"}, func(context.Context, ports.AgentEvent) error { return nil })
			if err != nil || answer.Outcome != ports.OutcomeAnswered || len(answer.Sources) != 1 || answer.Sources[0].DocumentID != *d.DocumentID || answer.Sources[0].Version != *d.DocumentVersion {
				t.Fatal(answer, err)
			}
			hrResponse[any](t, f.h, "PUT", f.path, in, f.owner, 409)
		})
	}
}

func TestPostgresResolutionAppendConflictAndConcurrentPublish(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f := prepareResolution(t, direction)
			first := f.save(t, knowledge.ResolutionInput{Title: "Базовая инструкция", Content: "Исходный текст"})
			first = f.publish(t, first.Revision, "publish")
			if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			// A second resolved source is a separate fixture conversation, sharing the published destination.
			var second string
			if _, err := f.db.Exec(`INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id) SELECT 'resolution_second_user',conversation_id,100,gen_random_uuid(),'user','completed','Дополнительный вопрос','test' FROM chat_messages WHERE id=$1`, f.question.MessageID); err != nil {
				t.Fatal(err)
			}
			if err := f.db.QueryRow(`INSERT INTO chat_messages(id,conversation_id,sequence,role,status,text,in_reply_to_message_id,trace_id) SELECT 'resolution_second',conversation_id,101,'assistant','completed','','resolution_second_user','test' FROM chat_messages WHERE id=$1 RETURNING id`, f.question.MessageID).Scan(&second); err != nil {
				t.Fatal(err)
			}
			var secondID string
			if err := f.db.QueryRow(`INSERT INTO knowledge_gaps(message_id,student_id,team_id,mentor_id,hr_id,step_id,question,context_excerpt,status,resolution,direction,original_direction) SELECT $2,student_id,team_id,mentor_id,hr_id,step_id,'Дополнительный вопрос','','resolved','Дополнительный ответ',direction,original_direction FROM knowledge_gaps WHERE id=$1 RETURNING id::text`, f.question.ID, second).Scan(&secondID); err != nil {
				t.Fatal(err)
			}
			f.path = "/" + direction + "/questions/" + secondID + "/instruction-draft"
			target := hrResponse[knowledge.ResolutionTarget](t, f.h, "GET", f.path+"/targets/"+*first.DocumentID, nil, f.owner, 200)
			if direction == "hr" {
				foreign := hrResponse[knowledge.HRDocument](t, f.h, "POST", "/hr/documents", knowledge.HRDocumentInput{Operation: hrKey(5697), Title: "Другая аудитория", Content: "Текст только другой команды", Teams: []string{"team_demo_frontend"}}, f.owner, 200)
				hrResponse[knowledge.HRDocument](t, f.h, "POST", "/hr/documents/"+foreign.ID+"/publish", map[string]any{"version": 1}, f.owner, 200)
				if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
					t.Fatal(err)
				}
				hrResponse[any](t, f.h, "GET", f.path+"/targets/"+foreign.ID, nil, f.owner, 404)
			} else {
				foreign := postMarkdown(t, f.h, f.other, hrKey(5698), "", "other.md", "Другая инструкция", 202)
				var doc knowledge.Upload
				if err := json.Unmarshal(foreign.Body.Bytes(), &doc); err != nil {
					t.Fatal(err)
				}
				if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
					t.Fatal(err)
				}
				hrResponse[any](t, f.h, "GET", f.path+"/targets/"+doc.ID, nil, f.owner, 404)
			}
			if target.Content != "Исходный текст" {
				t.Fatal(target)
			}
			in := knowledge.ResolutionInput{Title: target.Title, Content: "Дополнение", TargetID: &target.ID, BaseVersion: &target.Version}
			missing := hrKey(5699)
			bad := in
			bad.TargetID = &missing
			hrResponse[any](t, f.h, "PUT", f.path, bad, f.owner, 404)
			wrong := int64(999)
			bad = in
			bad.BaseVersion = &wrong
			hrResponse[any](t, f.h, "PUT", f.path, bad, f.owner, 409)
			if direction == "hr" {
				hrResponse[any](t, f.h, "GET", f.path+"/targets/"+missing, nil, f.owner, 404)
			}
			d := f.save(t, in)
			// Parallel editor changes destination after the draft was saved.
			if direction == "hr" {
				docPath := "/hr/documents/" + target.ID
				hrResponse[knowledge.HRDocument](t, f.h, "PATCH", docPath, knowledge.HRDocumentInput{Version: target.Version, Title: target.Title, Content: target.Content, Teams: target.Teams}, f.owner, 200)
				hrResponse[knowledge.HRDocument](t, f.h, "POST", docPath+"/publish", map[string]any{"version": 2}, f.owner, 200)
				if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else if _, err := f.db.Exec(`UPDATE mentor_documents SET version=version+1 WHERE id=$1`, target.ID); err != nil {
				t.Fatal(err)
			}
			hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 409)
			target = hrResponse[knowledge.ResolutionTarget](t, f.h, "GET", f.path+"/targets/"+target.ID, nil, f.owner, 200)
			in.Revision = d.Revision
			in.BaseVersion = &target.Version
			d = f.save(t, in)
			var wg sync.WaitGroup
			for range 2 {
				wg.Go(func() { f.publish(t, d.Revision, "publish") })
			}
			wg.Wait()
			if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			shared := hrResponse[struct{ Content string }](t, f.h, "GET", "/documents/"+target.ID, nil, f.student, 200)
			if shared.Content != "Исходный текст\n\nДополнение" {
				t.Fatal(shared)
			}
			// A revoked or superseded target cannot be read through its saved draft.
			if direction == "hr" {
				if _, err := f.db.Exec(`UPDATE hr_documents SET revoked=true WHERE id=$1`, target.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(`UPDATE mentor_documents SET version=version+1 WHERE id=$1`, target.ID); err != nil {
					t.Fatal(err)
				}
			}
			hrResponse[any](t, f.h, "GET", f.path, nil, f.owner, 404)
			hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 404)
		})
	}
}

func TestPostgresResolutionSourceChangeAndSizeLimit(t *testing.T) {
	f := prepareResolution(t, "mentor")
	in := knowledge.ResolutionInput{Title: "Текст", Content: ""}
	d := f.save(t, in)
	hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 409)
	in.Revision = d.Revision
	in.Content = "Новая инструкция"
	d = f.save(t, in)
	if _, err := f.db.Exec(`UPDATE knowledge_gaps SET revision=revision+1 WHERE id=$1`, f.question.ID); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 409)
	in.Revision = d.Revision
	d = f.save(t, in)
	d = f.publish(t, d.Revision, "publish")
	if _, err := f.db.Exec(`UPDATE knowledge_gaps SET revision=revision+1 WHERE id=$1`, f.question.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "POST", f.path+"/retry", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 409)
	// Combined base and addition must fit the existing ingestion bound.
	in.Revision = d.Revision
	in.Content = strings.Repeat("x", knowledge.MaxMarkdownBytes+1)
	hrResponse[any](t, f.h, "PUT", f.path, in, f.owner, 422)
}

func TestPostgresResolutionValidationAndRevocation(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f := prepareResolution(t, direction)
			for _, action := range []string{"publish", "retry"} {
				hrResponse[any](t, f.h, "POST", f.path+"/"+action, map[string]any{"revision": 1, "reviewed": true}, f.owner, 404)
			}
			hrResponse[any](t, f.h, "PUT", f.path, map[string]any{"revision": 0, "title": "test", "content": "x", "extra": "bad"}, f.owner, 422)
			hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": "wrong"}, f.owner, 422)
			in := knowledge.ResolutionInput{Title: "Ручной текст", Content: "token=secret-value"}
			d := f.save(t, in)
			hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": 999, "reviewed": true}, f.owner, 409)
			hrResponse[any](t, f.h, "POST", f.path+"/retry", map[string]any{"revision": 1, "reviewed": true}, f.owner, 409)
			hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 422)
			in.Revision = d.Revision
			in.Content = "Безопасный текст"
			d = f.save(t, in)
			hrResponse[any](t, f.h, "PUT", f.path, map[string]any{"revision": 2, "title": "", "content": "x"}, f.owner, 422)
			hrResponse[any](t, f.h, "GET", f.path+"/targets/bad", nil, f.owner, 404)
			d = f.publish(t, d.Revision, "publish")
			// Revoke the source while indexing is queued. No source/link/search leaks.
			if direction == "mentor" {
				if _, err := f.db.Exec(`DELETE FROM mentor_assignments WHERE intern_id='intern_demo_1'`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(`UPDATE hr_team_assignments SET active=false WHERE hr_user_id='hr_demo_1'`); err != nil {
					t.Fatal(err)
				}
			}
			hrResponse[any](t, f.h, "GET", f.path, nil, f.owner, 404)
			hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 404)
			if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(context.Background()); err != nil {
				t.Fatal(err)
			}
			hrResponse[any](t, f.h, "GET", "/documents/"+*d.DocumentID, nil, f.student, 404)
		})
	}
}

func TestPostgresResolutionGenerationFailureAndAccessChange(t *testing.T) {
	f := prepareResolution(t, "mentor")
	f.save(t, knowledge.ResolutionInput{Title: "Ручной ответ", Content: "Сохранённый текст"})
	f.h = requestHandler(f.db, Dependencies{Knowledge: f.service})
	hrResponse[any](t, f.h, "POST", f.path+"/generate", nil, f.owner, 503)
	if f.workspace(t).Draft.Content != "Сохранённый текст" {
		t.Fatal("manual text lost")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := f.db.Exec(`DELETE FROM mentor_assignments WHERE intern_id='intern_demo_1'`); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"title":"Инструкция","content":"Не показывать после отзыва"}`}}}})
	}))
	defer server.Close()
	f.h = requestHandler(f.db, Dependencies{Knowledge: f.service, ResolutionGenerator: knowledge.ResolutionGenerator{Client: providerhttp.Client{BaseURL: server.URL, Key: "fixture"}, Model: "test"}})
	hrResponse[any](t, f.h, "POST", f.path+"/generate", nil, f.owner, 404)
	hrResponse[any](t, f.h, "POST", f.path+"/generate", nil, f.owner, 404)
	hrResponse[any](t, f.h, "PUT", f.path, knowledge.ResolutionInput{Revision: 1, Title: "Нет доступа", Content: "x"}, f.owner, 404)
	hrResponse[any](t, f.h, "GET", f.path+"/targets/"+hrKey(1), nil, f.owner, 404)
}

func TestPostgresResolutionPublicationRollback(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f := prepareResolution(t, direction)
			d := f.save(t, knowledge.ResolutionInput{Title: "Публикация", Content: "Проверенный текст"})
			tables := []string{"mentor_documents", "mentor_document_versions", "resolution_drafts"}
			if direction == "hr" {
				tables = []string{"hr_documents", "hr_document_versions", "hr_document_teams", "resolution_drafts"}
			}
			if _, err := f.db.Exec(`CREATE FUNCTION resolution_test_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test storage failure'; END $$`); err != nil {
				t.Fatal(err)
			}
			for _, table := range tables {
				if _, err := f.db.Exec(`CREATE TRIGGER resolution_failure BEFORE INSERT OR UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION resolution_test_failure()`); err != nil {
					t.Fatal(err)
				}
				hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 503)
				if _, err := f.db.Exec(`DROP TRIGGER resolution_failure ON ` + table); err != nil {
					t.Fatal(err)
				}
				if w := f.workspace(t); w.Draft.DocumentID != nil || w.Draft.Status != "draft" {
					t.Fatal("partial commit", w)
				}
			}
			d = f.publish(t, d.Revision, "publish")
			for _, table := range []string{"knowledge_gap_history", "knowledge_gaps"} {
				if _, err := f.db.Exec(`CREATE TRIGGER resolution_failure BEFORE INSERT OR UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION resolution_test_failure()`); err != nil {
					t.Fatal(err)
				}
				if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
					t.Fatal(err)
				}
				hrResponse[any](t, f.h, "GET", "/documents/"+*d.DocumentID, nil, f.student, 404)
				if _, err := f.db.Exec(`DROP TRIGGER resolution_failure ON ` + table); err != nil {
					t.Fatal(err)
				}
				f.publish(t, d.Revision, "retry")
			}
			if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			hrResponse[any](t, f.h, "GET", "/documents/"+*d.DocumentID, nil, f.student, 200)
		})
	}
}

func TestPostgresResolutionCorruptStorageFailsClosed(t *testing.T) {
	f := prepareResolution(t, "mentor")
	in := knowledge.ResolutionInput{Title: "Инструкция", Content: "Текст"}
	d := f.save(t, in)
	if _, err := f.db.Exec(`UPDATE resolution_drafts SET payload='[]'::jsonb`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "GET", f.path, nil, f.owner, 503)
	in.Revision = d.Revision
	hrResponse[any](t, f.h, "PUT", f.path, in, f.owner, 503)
	hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": 1, "reviewed": true}, f.owner, 503)
	// Corruption or unavailability never exposes a fabricated successful response.
	if _, err := f.db.Exec(`DROP TABLE resolution_drafts`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "GET", f.path, nil, f.owner, 503)
	hrResponse[any](t, f.h, "PUT", f.path, in, f.owner, 503)
	hrResponse[any](t, f.h, "POST", f.path+"/publish", map[string]any{"revision": 1, "reviewed": true}, f.owner, 503)
}
