package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/actionqueue"
	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresActionQueue(t *testing.T) {
	db := testkit.Postgres(t)
	_, student, conversation := prepareDraft(t, db)
	h := requestHandler(db, Dependencies{QuestionDocuments: readyQuestionDocument{}})
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	otherMentor := loginCookie(t, h, "mentor_demo_2", nil)
	foreign := loginCookie(t, h, "intern_demo_other", nil)
	hr1 := loginCookie(t, h, "hr_demo_1", nil)
	hr2 := loginCookie(t, h, "hr_demo_2", nil)
	get := func(query string, cookie *http.Cookie) actionqueue.Page {
		t.Helper()
		return hrResponse[actionqueue.Page](t, h, "GET", "/action-queue"+query, nil, cookie, 200)
	}
	for _, query := range []string{"?limit=0", "?limit=51", "?limit=invalid", "?offset=-1", "?offset=x", "?kind=other"} {
		hrResponse[any](t, h, "GET", "/action-queue"+query, nil, mentor, 422)
	}
	hrResponse[any](t, h, "GET", "/action-queue", nil, nil, 401)
	hrResponse[any](t, h, "GET", "/action-queue", nil, student, 403)
	if page := get("", hr1); page.Total != 0 || page.Items == nil || page.Interns == nil {
		t.Fatal(page)
	}

	var step, assistant string
	if err := db.QueryRow("SELECT id FROM onboarding_steps WHERE plan_id='plan_demo_1' AND template_key='team-setup'").Scan(&step); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM chat_messages WHERE conversation_id=$1 AND role='assistant'", conversation).Scan(&assistant); err != nil {
		t.Fatal(err)
	}
	url := "/onboarding/steps/" + step + "/invitation"
	type invitationState struct{ Invitation *actionqueue.Invitation }
	if s := hrResponse[invitationState](t, h, "GET", url, nil, student, 200); s.Invitation != nil {
		t.Fatal(s)
	}
	hrResponse[any](t, h, "GET", url, nil, foreign, 404)
	hrResponse[any](t, h, "GET", url, nil, hr1, 403)
	hrResponse[any](t, h, "GET", "/onboarding/steps/step_local_setup/invitation", nil, student, 404)
	apiRequest(t, h, "PUT", "/api/v1"+url, `{`, student, 400)
	hrResponse[any](t, h, "PUT", url, map[string]string{"id": "bad", "action": "request"}, student, 422)
	hrResponse[any](t, h, "PUT", url, map[string]string{"id": hrKey(1), "action": "resolve"}, student, 422)

	create := map[string]string{"id": hrKey(1), "action": "request"}
	r := httptest.NewRequest("PUT", "/api/v1"+url, strings.NewReader(`{"id":"87654321-1234-4321-9876-000000000001","action":"request"}`))
	r.Header.Set("Origin", "https://foreign.invalid")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(student)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	for range 2 {
		s := hrResponse[invitationState](t, h, "PUT", url, create, student, 200).Invitation
		if s == nil || s.ID != hrKey(1) || s.State != "pending" {
			t.Fatal(s)
		}
	}
	hrResponse[any](t, h, "PUT", url, map[string]string{"id": hrKey(2), "action": "request"}, student, 409)

	questions := "/messages/" + assistant + "/questions"
	work := hrResponse[knowledgegaps.Question](t, h, "POST", questions,
		knowledgegaps.Operation{ID: hrKey(3), Draft: knowledgegaps.Draft{Direction: "mentor", Question: "Как открыть PR?"}}, student, 200)
	people := hrResponse[knowledgegaps.Question](t, h, "POST", questions,
		knowledgegaps.Operation{ID: hrKey(4), Draft: knowledgegaps.Draft{Direction: "hr", Question: "Как оформить отпуск?"}}, student, 200)
	replay := hrResponse[knowledgegaps.Question](t, h, "POST", questions,
		knowledgegaps.Operation{ID: hrKey(4), Draft: knowledgegaps.Draft{Direction: "hr", Question: "Как оформить отпуск?"}}, student, 200)
	if replay.ID != people.ID {
		t.Fatal(replay)
	}
	plan := hrResponse[onboarding.Plan](t, h, "GET", "/onboarding/plan", nil, student, 200)
	revision := checkpointStep(t, plan, step).Revision
	apiRequest(t, h, "PUT", internStepsURL+step+"/confirmation", checkpointBody(true, revision), student, 200)

	page := get("?intern_id=intern_demo_1&limit=1", mentor)
	if page.Counts["invitation"] != 1 || page.Counts["question"] != 1 || page.Counts["confirmation"] == 0 || page.Total < 3 || len(page.Items) != 1 {
		t.Fatal(page)
	}
	second := get("?intern_id=intern_demo_1&limit=1&offset=1", mentor)
	if second.Items[0].ID == page.Items[0].ID || second.Items[0].Since.Before(page.Items[0].Since) {
		t.Fatal(page, second)
	}
	if page := get("?kind=question", mentor); len(page.Items) != 1 || page.Items[0].ID != work.ID || page.Items[0].Action != "answer_question" {
		t.Fatal(page)
	}
	if page := get("?intern_id=intern_demo_1", otherMentor); page.Total != 0 {
		t.Fatal(page)
	}
	if page := get("", hr1); len(page.Items) != 1 || page.Items[0].ID != people.ID || page.Items[0].Action != "claim_question" {
		t.Fatal(page)
	}
	hrPath := "/hr/questions/" + people.ID
	claim := knowledgegaps.HRUpdate{ID: hrKey(5), Revision: people.Revision, Action: "claim"}
	hrResponse[knowledgegaps.Question](t, h, "PATCH", hrPath, claim, hr1, 200)
	claim.ID = hrKey(6)
	hrResponse[any](t, h, "PATCH", hrPath, claim, hr2, 409)
	if page := get("", hr2); page.Total != 0 {
		t.Fatal(page)
	}
	if page := get("", hr1); len(page.Items) != 1 || page.Items[0].Action != "answer_question" {
		t.Fatal(page)
	}
	if err := (hrscope.Store{DB: db}).Set(t.Context(), "hr_demo_1", "team_demo_backend", false); err != nil {
		t.Fatal(err)
	}
	if get("", hr1).Total != 0 || get("", hr2).Total != 1 {
		t.Fatal("revoked HR received work or the new owner did not")
	}
	claim.ID = hrKey(7)
	hrResponse[any](t, h, "PATCH", hrPath, claim, hr1, 404)
	latest := hrResponse[knowledgegaps.Question](t, h, "GET", hrPath, nil, hr2, 200)
	rerouted := hrResponse[knowledgegaps.Question](t, h, "POST", "/questions/"+people.ID+"/reroute",
		knowledgegaps.Operation{ID: hrKey(8), Revision: latest.Revision, Draft: knowledgegaps.Draft{Direction: "mentor", Question: "Вопрос перенаправлен наставнику"}}, student, 200)
	if rerouted.ID != people.ID || get("", hr2).Total != 0 || get("?kind=question", mentor).Total != 2 {
		t.Fatal("rerouting failed to move the existing queue item")
	}
	rerouted = hrResponse[knowledgegaps.Question](t, h, "POST", "/questions/"+people.ID+"/reroute",
		knowledgegaps.Operation{ID: hrKey(9), Revision: rerouted.Revision, Draft: knowledgegaps.Draft{Direction: "hr", Question: "Кадровое обращение"}}, student, 200)
	if rerouted.ID != people.ID || get("", hr2).Total != 1 || get("?kind=question", mentor).Total != 1 {
		t.Fatal("rerouting produced a duplicate")
	}

	mentorURL := "/mentor/interns/intern_demo_1/invitations/"
	hrResponse[any](t, h, "GET", mentorURL+"bad", nil, mentor, 404)
	hrResponse[any](t, h, "GET", mentorURL+hrKey(1), nil, hr2, 403)
	hrResponse[any](t, h, "GET", mentorURL+hrKey(1), nil, otherMentor, 404)
	apiRequest(t, h, "PUT", "/api/v1"+mentorURL+hrKey(1), `{`, mentor, 400)
	hrResponse[any](t, h, "PUT", mentorURL+hrKey(1), map[string]string{"note": strings.Repeat("я", 501)}, mentor, 422)
	cancel := map[string]string{"id": hrKey(1), "action": "cancel"}
	for range 2 {
		s := hrResponse[invitationState](t, h, "PUT", url, cancel, student, 200).Invitation
		if s.State != "canceled" || s.ClosedAt == nil {
			t.Fatal(s)
		}
	}
	hrResponse[any](t, h, "PUT", mentorURL+hrKey(1), map[string]string{}, mentor, 409)
	cancel["id"] = hrKey(90)
	hrResponse[any](t, h, "PUT", url, cancel, student, 409)
	create["id"] = hrKey(2)
	hrResponse[invitationState](t, h, "PUT", url, create, student, 200)

	if _, err := db.Exec("UPDATE mentor_assignments SET mentor_id='mentor_demo_2' WHERE intern_id='intern_demo_1'"); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, h, "PUT", mentorURL+hrKey(2), map[string]string{}, mentor, 404)
	h = requestHandler(db, Dependencies{QuestionDocuments: readyQuestionDocument{}})
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			req := httptest.NewRequest("PUT", "/api/v1"+mentorURL+hrKey(2), strings.NewReader(`{"note":"  Пригласил в командный канал  "}`))
			req.AddCookie(otherMentor)
			req.Header.Set("Origin", "http://localhost:5173")
			req.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			h.ServeHTTP(out, req)
			if out.Code != 200 {
				t.Error(out.Code, out.Body)
			}
		})
	}
	wg.Wait()
	result := hrResponse[actionqueue.Invitation](t, h, "PUT", mentorURL+hrKey(2), map[string]string{"note": "Другая заметка"}, otherMentor, 200)
	if result.State != "completed" || result.ClosedAt == nil || result.Note == nil || *result.Note != "Пригласил в командный канал" {
		t.Fatal(result)
	}
	if got := hrResponse[invitationState](t, h, "GET", url, nil, student, 200).Invitation; got == nil || got.State != "completed" {
		t.Fatal(got)
	}
	if get("?kind=invitation", otherMentor).Total != 0 {
		t.Fatal("completed invitation still visible")
	}

	plan = hrResponse[onboarding.Plan](t, h, "GET", "/mentor/interns/intern_demo_1/plan", nil, otherMentor, 200)
	revision = checkpointStep(t, plan, step).Revision
	apiRequest(t, h, "PUT", mentorInternURL+"/steps/"+step+"/confirmation", checkpointBody(true, revision), otherMentor, 200)
	if slices.ContainsFunc(get("?kind=confirmation&intern_id=intern_demo_1", otherMentor).Items, func(item actionqueue.Item) bool { return item.ID == step }) {
		t.Fatal("confirmed checkpoint still visible")
	}
	apiRequest(t, h, "PATCH", "/api/v1/mentor/questions/"+work.ID,
		`{"status":"resolved","revision":1,"resolution":"Готово","document_ids":["ready"]}`, otherMentor, 409)
	h = requestHandler(db, Dependencies{QuestionDocuments: testkitQuestionDocument{}})
	apiRequest(t, h, "PATCH", "/api/v1/mentor/questions/"+work.ID,
		`{"status":"resolved","revision":1,"resolution":"Готово","document_ids":["ready"]}`, otherMentor, 200)
	if get("?kind=question", otherMentor).Total != 0 {
		t.Fatal("resolved question still visible")
	}
	var actions int
	if err := db.QueryRow(`SELECT count(*) FROM action_queue_completions WHERE appeared_at <= completed_at`).Scan(&actions); err != nil || actions != 4 {
		t.Fatalf("completion metrics: count=%d err=%v", actions, err)
	}
}

type testkitQuestionDocument struct{}

func (testkitQuestionDocument) CheckReady(context.Context, *sql.Tx, identity.Principal, identity.Principal, []string) error {
	return nil
}

type queueAccess struct{ err error }

func (a queueAccess) Check(context.Context, ports.AccessCheck) (bool, error) { return false, a.err }

func TestPostgresActionQueueSourceUnavailable(t *testing.T) {
	db := testkit.Postgres(t)
	_, _, _ = prepareDraft(t, db)
	if _, err := db.Exec(`INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,completion_policy,resource_id,permission)
VALUES ('queue_grant','plan_demo_1',501,'Доступ','','not_started','mentor','repo','read')`); err != nil {
		t.Fatal(err)
	}
	for _, access := range []queueAccess{{}, {err: errors.New("provider details should never leak")}} {
		h := requestHandler(db, Dependencies{Access: access})
		mentor := loginCookie(t, h, "mentor_demo_1", nil)
		if access.err != nil {
			w := apiRequest(t, h, "GET", "/api/v1/action-queue", "", mentor, 503)
			if strings.Contains(w.Body.String(), access.err.Error()) {
				t.Fatal(w.Body)
			}
		} else {
			p := hrResponse[actionqueue.Page](t, h, "GET", "/action-queue?kind=confirmation", nil, mentor, 200)
			if slices.ContainsFunc(p.Items, func(item actionqueue.Item) bool { return item.ID == "queue_grant" }) {
				t.Fatal("checkpoint is not actionable without its grant")
			}
		}
	}
	h := requestHandler(db, Dependencies{})
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	if _, err := db.Exec("DROP TABLE checkpoint_invitation_requests"); err != nil {
		t.Fatal(err)
	}
	hrResponse[any](t, h, "GET", "/action-queue", nil, mentor, 503)
}
