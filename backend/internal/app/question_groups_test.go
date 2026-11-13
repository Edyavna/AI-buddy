package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func TestPostgresQuestionGroupOwnershipTransfer(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f, ids := prepareQuestionGroup(t, direction)
			old := createQuestionGroup(t, f, ids, 58015)
			if direction == "mentor" {
				if _, err := f.db.Exec(`UPDATE mentor_assignments SET mentor_id='mentor_demo_2' WHERE intern_id IN ('intern_demo_1','intern_demo_2')`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(`INSERT INTO hr_team_assignments(hr_user_id,team_id,active) VALUES('hr_demo_2','team_demo_backend',true) ON CONFLICT(hr_user_id,team_id) DO UPDATE SET active=true; UPDATE knowledge_gaps SET hr_id='hr_demo_2',revision=revision+1 WHERE direction='hr'`); err != nil {
					t.Fatal(err)
				}
			}
			hrResponse[any](t, f.h, "GET", groupPath(f, old.ID), nil, f.owner, 404)
			f.owner = f.other
			w := hrResponse[knowledge.GroupWorkspace](t, f.h, "GET", groupPath(f, ""), nil, f.owner, 200)
			if len(w.Candidates) != 2 || len(w.Groups) != 0 {
				t.Fatal(w)
			}
			g := createQuestionGroup(t, f, ids, 58016)
			if len(g.Members) != 2 {
				t.Fatal(g)
			}
			var active, events int
			if err := f.db.QueryRow(`SELECT count(*) FROM question_group_members WHERE group_id=$1 AND removed_at IS NULL`, old.ID).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if err := f.db.QueryRow(`SELECT count(*) FROM question_group_history WHERE group_id=$1 AND event='owner_changed'`, old.ID).Scan(&events); err != nil || active != 0 || events != 2 {
				t.Fatal(active, events, err)
			}
		})
	}
}

func TestPostgresQuestionGroupUnavailablePublicationAndArchive(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f, ids := prepareQuestionGroup(t, direction)
			g := publishQuestionGroup(t, f, createQuestionGroup(t, f, ids, 58017))
			if direction == "hr" {
				if _, err := f.db.Exec(`UPDATE hr_documents SET revoked=true WHERE id=$1`, *g.Draft.DocumentID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(`UPDATE mentor_documents SET version=version+1 WHERE id=$1`, *g.Draft.DocumentID); err != nil {
					t.Fatal(err)
				}
			}
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
			if g.PublicationError == nil || g.Draft != nil || len(g.Members) != 2 || len(g.History) == 0 {
				t.Fatal(g)
			}
			hrResponse[any](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 409)
			body := knowledge.GroupInput{ID: g.ID, Revision: g.Revision, Title: g.Title, Resolution: g.Resolution, QuestionIDs: []string{}}
			archived := hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 200)
			if len(archived.Members) != 0 || len(archived.History) < 2 {
				t.Fatal(archived)
			}
			hrResponse[any](t, f.h, "GET", groupPath(f, g.ID)+"/instruction-draft", nil, f.owner, 409)
			fresh := createQuestionGroup(t, f, ids, 58018)
			if fresh.ID == g.ID || fresh.Draft != nil {
				t.Fatal("implicit merge", fresh)
			}
		})
	}
}

func TestPostgresQuestionGroupArchiveBeforeIndexing(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "mentor")
	g := createQuestionGroup(t, f, ids, 58019)
	path := groupPath(f, g.ID) + "/instruction-draft"
	d := hrResponse[knowledge.ResolutionDraft](t, f.h, "PUT", path, knowledge.ResolutionInput{Title: g.Title, Content: g.Resolution}, f.owner, 200)
	hrResponse[knowledge.ResolutionDraft](t, f.h, "POST", path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 200)
	body := knowledge.GroupInput{ID: g.ID, Revision: g.Revision, Title: g.Title, Resolution: g.Resolution, QuestionIDs: []string{}}
	hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 200)
	if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
	if g.PublishedAt != nil || len(g.Members) != 0 || g.Draft.Status != "failed" {
		t.Fatal(g)
	}
}

func prepareQuestionGroup(t *testing.T, direction string) (resolutionFixture, []string) {
	t.Helper()
	f := prepareResolution(t, direction)
	path := "/" + direction + "/questions/" + f.question.ID
	if direction == "hr" {
		f.question = hrResponse[knowledgegaps.Question](t, f.h, "PATCH", path, knowledgegaps.HRUpdate{ID: hrKey(58001), Revision: f.question.Revision, Action: "reopen"}, f.owner, 200)
	} else {
		f.question = hrResponse[knowledgegaps.Question](t, f.h, "PATCH", path, knowledgegaps.Update{State: "in_progress", Revision: f.question.Revision}, f.owner, 200)
	}
	if _, err := f.db.Exec(`UPDATE mentor_assignments SET mentor_id='mentor_demo_1' WHERE intern_id='intern_demo_2'`); err != nil {
		t.Fatal(err)
	}
	first := f.question.ID
	second := addGroupQuestion(t, f, "intern_demo_2", "Как оформить доступ к сервису?")
	if _, err := f.db.Exec(`UPDATE knowledge_gaps SET question='Как оформить доступ к сервису?' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	return f, []string{first, second}
}
func addGroupQuestion(t *testing.T, f resolutionFixture, student, text string) string {
	t.Helper()
	var user, assistant, id string
	if err := f.db.QueryRow(`INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id) SELECT gen_random_uuid()::text,conversation_id,(SELECT max(sequence)+1 FROM chat_messages WHERE conversation_id=m.conversation_id),gen_random_uuid(),'user','completed',$2,'test' FROM chat_messages m WHERE id=$1 RETURNING id`, f.question.MessageID, text).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`INSERT INTO chat_messages(id,conversation_id,sequence,role,status,text,in_reply_to_message_id,trace_id) SELECT gen_random_uuid()::text,conversation_id,sequence+1,'assistant','completed','',$2,'test' FROM chat_messages WHERE id=$1 RETURNING id`, user, user).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`INSERT INTO knowledge_gaps(message_id,student_id,team_id,mentor_id,hr_id,question,context_excerpt,status,direction,original_direction) SELECT $2,$3,(SELECT team_id FROM users WHERE id=$3),mentor_id,hr_id,$4,'Личный контекст не переносится в общий ответ','in_progress',direction,original_direction FROM knowledge_gaps WHERE id=$1 RETURNING id::text`, f.question.ID, assistant, student, text).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func groupPath(f resolutionFixture, id string) string {
	path := "/" + f.direction + "/question-groups"
	if id != "" {
		path += "/" + id
	}
	return path
}
func createQuestionGroup(t *testing.T, f resolutionFixture, ids []string, key int) knowledge.QuestionGroup {
	return hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, ""), knowledge.GroupInput{ID: hrKey(key), Title: "Оформить доступ к сервису", Resolution: "Создайте заявку и дождитесь согласования. Личные сведения не включаются.", QuestionIDs: ids}, f.owner, 200)
}
func publishQuestionGroup(t *testing.T, f resolutionFixture, g knowledge.QuestionGroup) knowledge.QuestionGroup {
	t.Helper()
	path := groupPath(f, g.ID) + "/instruction-draft"
	hrResponse[knowledge.ResolutionWorkspace](t, f.h, "GET", path, nil, f.owner, 200)
	hrResponse[knowledge.ResolutionSuggestion](t, f.h, "POST", path+"/generate", nil, f.owner, 200)
	d := hrResponse[knowledge.ResolutionDraft](t, f.h, "PUT", path, knowledge.ResolutionInput{Title: g.Title, Content: "# Доступ\nСоздайте заявку и дождитесь согласования."}, f.owner, 200)
	hrResponse[knowledge.ResolutionDraft](t, f.h, "POST", path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 200)
	if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	return hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
}

func TestPostgresQuestionGroupLifecycle(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f, ids := prepareQuestionGroup(t, direction)
			w := hrResponse[knowledge.GroupWorkspace](t, f.h, "GET", groupPath(f, ""), nil, f.owner, 200)
			if len(w.Suggestions) == 0 {
				t.Fatal(w)
			}
			hrResponse[any](t, f.h, "GET", groupPath(f, ""), nil, f.student, 403)
			g := createQuestionGroup(t, f, ids, 58002)
			w = hrResponse[knowledge.GroupWorkspace](t, f.h, "GET", groupPath(f, ""), nil, f.owner, 200)
			if len(w.Groups) != 1 || len(w.Candidates) != 0 {
				t.Fatal(w)
			}
			if len(g.Members) != 2 || len(g.History) != 1 {
				t.Fatal(g)
			}
			again := createQuestionGroup(t, f, ids, 58002)
			if again.ID != g.ID {
				t.Fatal("duplicate group")
			}
			hrResponse[any](t, f.h, "GET", groupPath(f, g.ID), nil, f.other, 404)
			hrResponse[any](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 409)
			g = publishQuestionGroup(t, f, g)
			if g.Draft.Status != "ready" || g.PublishedAt == nil {
				t.Fatal(g)
			}
			for _, m := range g.Members {
				if m.Status != "pending" || m.Question.Status == "resolved" {
					t.Fatal("automatic delivery/closure", m)
				}
			}
			for range 2 {
				g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
			}
			if g.DeliveredCount != 2 {
				t.Fatal(g)
			}
			for _, qid := range ids {
				var raw []byte
				if err := f.db.QueryRow(`SELECT document_ids FROM knowledge_gaps WHERE id=$1`, qid).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				if strings.Count(string(raw), *g.Draft.DocumentID) != 1 {
					t.Fatal(string(raw))
				}
			}
			f.h = requestHandler(f.db, Dependencies{Knowledge: f.service, ResolutionGenerator: knowledge.ResolutionGenerator{Demo: true}})
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
			hrResponse[any](t, f.h, "GET", "/documents/"+*g.Draft.DocumentID, nil, f.student, 200)
			hrResponse[any](t, f.h, "GET", "/documents/"+*g.Draft.DocumentID, nil, f.foreign, 404)
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/close", map[string]any{"revision": g.Revision}, f.owner, 200)
			for _, m := range g.Members {
				if m.Status != "closed" || m.Question.Status != "resolved" {
					t.Fatal(g)
				}
			}
			hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/close", map[string]any{"revision": g.Revision}, f.owner, 200)
			addGroupQuestion(t, f, "intern_demo_1", "Как оформить доступ к сервису?")
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
			if g.RepeatCount != 1 {
				t.Fatal(g.RepeatCount)
			}
		})
	}
}

func TestPostgresQuestionGroupPartialDelivery(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f, ids := prepareQuestionGroup(t, direction)
			g := publishQuestionGroup(t, f, createQuestionGroup(t, f, ids, 58003))
			// Same text routed to another specialist must immediately lose all personal details.
			if _, err := f.db.Exec(`UPDATE knowledge_gaps SET direction=CASE WHEN direction='hr' THEN 'mentor' ELSE 'hr' END,mentor_id=CASE WHEN direction='hr' THEN 'mentor_demo_1' ELSE NULL END,hr_id=CASE WHEN direction='mentor' THEN 'hr_demo_1' ELSE NULL END,revision=revision+1 WHERE id=$1`, ids[1]); err != nil {
				t.Fatal(err)
			}
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
			if g.DeliveredCount != 1 {
				t.Fatal(g)
			}
			for _, m := range g.Members {
				if m.ID == ids[1] && (m.Question != nil || m.Status != "failed") {
					t.Fatal(m)
				}
			}
			if _, err := f.db.Exec(`UPDATE knowledge_gaps SET direction=$2,mentor_id=CASE WHEN $2='mentor' THEN 'mentor_demo_1' ELSE NULL END,hr_id=CASE WHEN $2='hr' THEN 'hr_demo_1' ELSE NULL END WHERE id=$1`, ids[1], direction); err != nil {
				t.Fatal(err)
			}
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), knowledge.GroupInput{ID: g.ID, Revision: g.Revision, Title: g.Title, Resolution: g.Resolution, QuestionIDs: ids}, f.owner, 200)
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
			if g.DeliveredCount != 2 {
				t.Fatal(g)
			}
			// Removing a member does not retract an already public team instruction.
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), knowledge.GroupInput{ID: g.ID, Revision: g.Revision, Title: g.Title, Resolution: g.Resolution, QuestionIDs: ids[:1]}, f.owner, 200)
			if len(g.Members) != 1 {
				t.Fatal(g)
			}
		})
	}
}

func TestPostgresQuestionGroupConflictAndValidation(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "mentor")
	g := createQuestionGroup(t, f, ids, 58004)
	in := knowledge.GroupInput{ID: hrKey(58005), Title: g.Title, Resolution: g.Resolution, QuestionIDs: ids}
	hrResponse[any](t, f.h, "POST", groupPath(f, ""), in, f.owner, 409)
	in.QuestionIDs = []string{ids[0], ids[0]}
	hrResponse[any](t, f.h, "POST", groupPath(f, ""), in, f.owner, 422)
	in.QuestionIDs = []string{"invalid"}
	hrResponse[any](t, f.h, "POST", groupPath(f, ""), in, f.owner, 422)
	in.QuestionIDs = ids
	in.ID = g.ID
	in.Revision = 999
	hrResponse[any](t, f.h, "PUT", groupPath(f, g.ID), in, f.owner, 409)
	in.Revision = g.Revision
	in.QuestionIDs = ids[:1]
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), in, f.owner, 200)
	in.ID = hrKey(58005)
	in.Revision = 0
	in.QuestionIDs = ids[1:]
	createQuestionGroup(t, f, in.QuestionIDs, 58005)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { createQuestionGroup(t, f, ids, 58004) })
	}
	wg.Wait()
}

func TestPostgresQuestionGroupStorageFailures(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "mentor")
	if _, err := f.db.Exec(`CREATE FUNCTION group_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test storage failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	body := knowledge.GroupInput{ID: hrKey(58006), Title: "Доступ к сервису", Resolution: "Общее решение", QuestionIDs: ids}
	for _, table := range []string{"question_groups", "question_group_members", "question_group_history"} {
		if _, err := f.db.Exec(`CREATE TRIGGER group_failure BEFORE INSERT ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION group_test_fail()`); err != nil {
			t.Fatal(err)
		}
		hrResponse[any](t, f.h, "POST", groupPath(f, ""), body, f.owner, 503)
		if _, err := f.db.Exec(`DROP TRIGGER group_failure ON ` + table); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := f.db.QueryRow(`SELECT count(*) FROM question_groups WHERE id=$1`, body.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal(count, err)
		}
	}
	g := createQuestionGroup(t, f, ids, 58006)
	body.ID = g.ID
	body.Revision = g.Revision
	for _, table := range []string{"question_groups", "question_group_members"} {
		if _, err := f.db.Exec(`CREATE TRIGGER group_failure BEFORE UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION group_test_fail()`); err != nil {
			t.Fatal(err)
		}
		hrResponse[any](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 503)
		if _, err := f.db.Exec(`DROP TRIGGER group_failure ON ` + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`UPDATE question_group_history SET question_ids='{}'::jsonb`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 503)
	if _, err := f.db.Exec(`DROP TABLE question_group_history`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 503)
	hrResponse[any](t, f.h, "GET", groupPath(f, ""), nil, f.owner, 503)
}

func TestPostgresQuestionGroupRecipientFailureIsIndependent(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "mentor")
	g := publishQuestionGroup(t, f, createQuestionGroup(t, f, ids, 58007))
	if _, err := f.db.Exec(`CREATE FUNCTION group_recipient_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.question_id='` + ids[1] + `'::uuid THEN RAISE EXCEPTION 'test recipient storage error'; END IF; RETURN NEW; END $$; CREATE TRIGGER recipient_failure BEFORE INSERT ON knowledge_gap_history FOR EACH ROW EXECUTE FUNCTION group_recipient_fail()`); err != nil {
		t.Fatal(err)
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
	if g.DeliveredCount != 1 {
		t.Fatal(g)
	}
	if _, err := f.db.Exec(`DROP TRIGGER recipient_failure ON knowledge_gap_history`); err != nil {
		t.Fatal(err)
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
	if g.DeliveredCount != 2 {
		t.Fatal(g)
	}
}

func TestPostgresQuestionGroupMetricsAndLimits(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "mentor")
	g := publishQuestionGroup(t, f, createQuestionGroup(t, f, ids, 58008))
	for _, statement := range []string{
		`INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id) SELECT 'group_repeat_user_'||n,m.conversation_id,1000+n*2,gen_random_uuid(),'user','completed','Как оформить доступ к сервису?','test' FROM chat_messages m CROSS JOIN generate_series(1,501)n WHERE m.id=$1`,
		`INSERT INTO chat_messages(id,conversation_id,sequence,role,status,text,in_reply_to_message_id,trace_id) SELECT 'group_repeat_assistant_'||n,m.conversation_id,1001+n*2,'assistant','completed','','group_repeat_user_'||n,'test' FROM chat_messages m CROSS JOIN generate_series(1,501)n WHERE m.id=$1`,
		`INSERT INTO knowledge_gaps(message_id,student_id,team_id,mentor_id,question,context_excerpt,status) SELECT 'group_repeat_assistant_'||n,q.student_id,q.team_id,q.mentor_id,'Как оформить доступ к сервису?','','new' FROM knowledge_gaps q CROSS JOIN generate_series(1,501)n WHERE q.message_id=$1`,
	} {
		if _, err := f.db.Exec(statement, f.question.MessageID); err != nil {
			t.Fatal(err)
		}
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
	if g.RepeatCount != 500 || !g.RepeatCountLimited {
		t.Fatal(g.RepeatCount, g.RepeatCountLimited)
	}
	w := hrResponse[knowledge.GroupWorkspace](t, f.h, "GET", groupPath(f, ""), nil, f.owner, 200)
	if !w.Limited || len(w.Candidates) != 100 || len(w.Suggestions) != 30 {
		t.Fatal(w.Limited, len(w.Candidates), len(w.Suggestions))
	}
}

func TestPostgresQuestionGroupUnavailableStoreAndDeniedRoles(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "mentor")
	s := knowledge.Uploads{DB: f.db}
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor})
	intern := identity.WithPrincipal(context.Background(), identity.Principal{UserID: "intern_demo_1", TeamID: "team_demo_backend", Role: identity.Intern})
	if _, err := s.GroupWorkspace(intern); err != ports.ErrForbidden {
		t.Fatal(err)
	}
	f.db.Close()
	if _, err := s.GroupWorkspace(ctx); err == nil {
		t.Fatal("unavailable store accepted")
	}
	if _, err := s.GetGroup(ctx, hrKey(58002)); err == nil {
		t.Fatal("unavailable group accepted")
	}
	if _, err := s.SaveGroup(ctx, "", knowledge.GroupInput{ID: hrKey(58009), Title: "Тема", QuestionIDs: ids}); err == nil {
		t.Fatal("unavailable save accepted")
	}
	if _, err := s.DeliverGroup(ctx, hrKey(58002), 1, false); err == nil {
		t.Fatal("unavailable delivery accepted")
	}
}

func TestPostgresQuestionGroupBusinessTransitions(t *testing.T) {
	for _, direction := range []string{"mentor", "hr"} {
		t.Run(direction, func(t *testing.T) {
			f, ids := prepareQuestionGroup(t, direction)
			body := knowledge.GroupInput{ID: hrKey(58010), Title: "Доступ к сервису", QuestionIDs: ids}
			g := hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, ""), body, f.owner, 200)
			draftPath := groupPath(f, g.ID) + "/instruction-draft"
			hrResponse[any](t, f.h, "GET", draftPath, nil, f.owner, 409)
			hrResponse[any](t, f.h, "GET", groupPath(f, hrKey(58099))+"/instruction-draft", nil, f.owner, 404)
			bad := body
			bad.ID = "not-a-uuid"
			hrResponse[any](t, f.h, "POST", groupPath(f, ""), bad, f.owner, 422)
			bad = body
			bad.Title = "Другая тема"
			hrResponse[any](t, f.h, "POST", groupPath(f, ""), bad, f.owner, 409)
			hrResponse[any](t, f.h, "POST", groupPath(f, ""), body, f.other, 404)
			body.Revision = 1
			body.Resolution = "Общий проверенный ответ"
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 200)
			hrResponse[any](t, f.h, "PUT", groupPath(f, hrKey(58098)), body, f.owner, 404)
			hrResponse[any](t, f.h, "POST", groupPath(f, ""), map[string]any{"extra": true}, f.owner, 422)
			hrResponse[any](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": "wrong"}, f.owner, 422)
			hrResponse[any](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": 0}, f.owner, 422)
			d := hrResponse[knowledge.ResolutionDraft](t, f.h, "PUT", draftPath, knowledge.ResolutionInput{Title: g.Title, Content: g.Resolution}, f.owner, 200)
			if _, err := f.db.Exec(`UPDATE knowledge_gaps SET status='resolved' WHERE id=$1`, ids[1]); err != nil {
				t.Fatal(err)
			}
			hrResponse[any](t, f.h, "POST", draftPath+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 409)
			bad = body
			bad.Revision = g.Revision
			hrResponse[any](t, f.h, "PUT", groupPath(f, g.ID), bad, f.owner, 409)
			fresh := body
			fresh.ID = hrKey(58011)
			fresh.Revision = 0
			hrResponse[any](t, f.h, "POST", groupPath(f, ""), fresh, f.owner, 409)
			if _, err := f.db.Exec(`UPDATE knowledge_gaps SET status='in_progress' WHERE id=$1`, ids[1]); err != nil {
				t.Fatal(err)
			}
			hrResponse[knowledge.ResolutionDraft](t, f.h, "POST", draftPath+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 200)
			bad = body
			bad.Revision = g.Revision
			hrResponse[any](t, f.h, "PUT", groupPath(f, g.ID), bad, f.owner, 409)
			bad.Title = "Изменённый текст"
			hrResponse[any](t, f.h, "PUT", groupPath(f, g.ID), bad, f.owner, 409)
			if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			extra := addGroupQuestion(t, f, "intern_demo_1", "Дополнительный вопрос")
			bad = body
			bad.Revision = g.Revision
			bad.QuestionIDs = append(append([]string{}, ids...), extra)
			hrResponse[any](t, f.h, "PUT", groupPath(f, g.ID), bad, f.owner, 409)
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/close", map[string]any{"revision": g.Revision}, f.owner, 200)
			if g.DeliveredCount != 0 {
				t.Fatal(g)
			}
			if _, err := f.db.Exec(`UPDATE knowledge_gaps SET revision=revision+1 WHERE id=$1`, ids[0]); err != nil {
				t.Fatal(err)
			}
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
			if g.DeliveredCount != 1 {
				t.Fatal(g)
			}
			body.Revision = g.Revision
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 200)
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
			g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/close", map[string]any{"revision": g.Revision}, f.owner, 200)
			body.Revision = g.Revision
			hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 200)
			if direction == "mentor" {
				ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "mentor_demo_1", TeamID: "another-team", Role: identity.Mentor})
				if _, err := (knowledge.Uploads{DB: f.db}).GetGroup(ctx, g.ID); err != ports.ErrNotFound {
					t.Fatal(err)
				}
				if _, err := f.db.Exec(`DELETE FROM mentor_assignments WHERE mentor_id='mentor_demo_1'`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(`UPDATE hr_team_assignments SET active=false WHERE hr_user_id='hr_demo_1'`); err != nil {
					t.Fatal(err)
				}
			}
			hrResponse[any](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 404)
			w := hrResponse[knowledge.GroupWorkspace](t, f.h, "GET", groupPath(f, ""), nil, f.owner, 200)
			if len(w.Groups) != 0 {
				t.Fatal(w)
			}
		})
	}
}

func TestPostgresQuestionGroupAudiencesAndRecordFailure(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "hr")
	other := addGroupQuestion(t, f, "intern_demo_other", "Как оформить доступ к сервису?")
	w := hrResponse[knowledge.GroupWorkspace](t, f.h, "GET", groupPath(f, ""), nil, f.owner, 200)
	for _, s := range w.Suggestions {
		for _, id := range s.QuestionIDs {
			if id == other {
				t.Fatal("cross-team suggestion")
			}
		}
	}
	g := createQuestionGroup(t, f, ids, 58012)
	body := knowledge.GroupInput{ID: g.ID, Revision: g.Revision, Title: g.Title, Resolution: g.Resolution, QuestionIDs: []string{other}}
	hrResponse[any](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 409)
	body.ID = hrKey(58013)
	body.Revision = 0
	body.QuestionIDs = []string{ids[0], other}
	hrResponse[any](t, f.h, "POST", groupPath(f, ""), body, f.owner, 409)
	g = publishQuestionGroup(t, f, g)
	if _, err := f.db.Exec(`UPDATE knowledge_gaps SET student_id='intern_demo_other',team_id='team_demo_frontend',revision=revision+1 WHERE id=$1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
	for _, m := range g.Members {
		if m.ID == ids[1] && m.Question != nil {
			t.Fatal("moved participant disclosed")
		}
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
	if g.DeliveredCount != 1 {
		t.Fatal(g)
	}
	if _, err := f.db.Exec(`UPDATE knowledge_gaps SET student_id='intern_demo_2',team_id='team_demo_backend' WHERE id=$1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	body = knowledge.GroupInput{ID: g.ID, Revision: g.Revision, Title: g.Title, Resolution: g.Resolution, QuestionIDs: ids}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "PUT", groupPath(f, g.ID), body, f.owner, 200)
	if _, err := f.db.Exec(`CREATE FUNCTION group_member_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.question_id='` + ids[1] + `'::uuid THEN RAISE EXCEPTION 'test member update failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER member_failure BEFORE UPDATE ON question_group_members FOR EACH ROW EXECUTE FUNCTION group_member_fail()`); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 503)
	if _, err := f.db.Exec(`DROP TRIGGER member_failure ON question_group_members`); err != nil {
		t.Fatal(err)
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
	if g.DeliveredCount != 2 {
		t.Fatal(g)
	}
}

func TestPostgresQuestionGroupIndexRetryAndConcurrentDelivery(t *testing.T) {
	f, ids := prepareQuestionGroup(t, "hr")
	g := createQuestionGroup(t, f, ids, 58014)
	path := groupPath(f, g.ID) + "/instruction-draft"
	d := hrResponse[knowledge.ResolutionDraft](t, f.h, "PUT", path, knowledge.ResolutionInput{Title: g.Title, Content: g.Resolution}, f.owner, 200)
	hrResponse[knowledge.ResolutionDraft](t, f.h, "POST", path+"/publish", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 200)
	if _, err := (knowledge.UploadWorker{Knowledge: &knowledge.Service{DB: f.db, Embedder: uploadEmbedder{fail: true}}}).RunOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
	if g.Draft.Status != "failed" || g.PublishedAt != nil {
		t.Fatal(g)
	}
	hrResponse[knowledge.ResolutionDraft](t, f.h, "POST", path+"/retry", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 200)
	if _, err := f.db.Exec(`CREATE FUNCTION group_index_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test publication failure'; END $$; CREATE TRIGGER publication_failure BEFORE UPDATE ON question_groups FOR EACH ROW EXECUTE FUNCTION group_index_fail()`); err != nil {
		t.Fatal(err)
	}
	if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DROP TRIGGER publication_failure ON question_groups`); err != nil {
		t.Fatal(err)
	}
	hrResponse[knowledge.ResolutionDraft](t, f.h, "POST", path+"/retry", map[string]any{"revision": d.Revision, "reviewed": true}, f.owner, 200)
	if _, err := (knowledge.UploadWorker{Knowledge: f.service}).RunOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			hrResponse[knowledge.QuestionGroup](t, f.h, "POST", groupPath(f, g.ID)+"/deliver", map[string]any{"revision": g.Revision}, f.owner, 200)
		})
	}
	wg.Wait()
	g = hrResponse[knowledge.QuestionGroup](t, f.h, "GET", groupPath(f, g.ID), nil, f.owner, 200)
	if g.DeliveredCount != 2 {
		t.Fatal(g)
	}
	for _, id := range ids {
		var count int
		if err := f.db.QueryRow(`SELECT count(*) FROM question_group_history WHERE group_id=$1 AND event='group_instruction_delivered' AND question_ids ? $2`, g.ID, id).Scan(&count); err != nil || count != 1 {
			t.Fatal(count, err)
		}
	}
}
