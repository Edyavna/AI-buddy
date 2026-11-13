package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/mentor"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

const (
	internStepsURL  = "/api/v1/onboarding/steps/"
	mentorInternURL = "/api/v1/mentor/interns/intern_demo_1"
)

func checkpointBody(confirmed bool, revision int64) string {
	return fmt.Sprintf(`{"confirmed":%t,"revision":%d}`, confirmed, revision)
}

func checkpointStep(t *testing.T, plan onboarding.Plan, id string) onboarding.Step {
	t.Helper()
	for _, step := range plan.Steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("checkpoint %s missing: %+v", id, plan)
	return onboarding.Step{}
}

func assertCheckpointError(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	var response struct {
		Error struct {
			Code    string
			TraceID string `json:"trace_id"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != code || response.Error.TraceID == "" {
		t.Fatalf("want %s, got %s: %v", code, w.Body.String(), err)
	}
}

func TestPostgresCheckpointPoliciesAndRecovery(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,completion_policy,template_key,period,instructions,resources)
VALUES ('step_intern','plan_demo_1',101,'Стажёр','Описание','not_started','intern','intern_task','1 неделя','Инструкция',
        '[{"title":"Шаблон","url":null,"note":"У наставника"}]'),
       ('step_mentor','plan_demo_1',102,'Наставник','','not_started','mentor',NULL,NULL,NULL,'[]'),
       ('step_both','plan_demo_1',103,'Вместе','','not_started','both',NULL,NULL,NULL,'[]'),
       ('step_both_reverse','plan_demo_1',104,'Вместе','','not_started','both',NULL,NULL,NULL,'[]'),
       ('step_dependent','plan_demo_1',105,'После обоих','','not_started','both',NULL,NULL,NULL,'[]');
INSERT INTO onboarding_step_dependencies(plan_id,step_id,prerequisite_id)
VALUES ('plan_demo_1','step_dependent','step_both');`); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	intern := loginCookie(t, h, "intern_demo_1", nil)
	assigned := loginCookie(t, h, "mentor_demo_1", nil)
	unassigned := loginCookie(t, h, "mentor_demo_2", nil)
	foreign := loginCookie(t, h, "intern_demo_other", nil)
	internURL := func(id string) string { return internStepsURL + id + "/confirmation" }
	mentorURL := func(id string) string { return mentorInternURL + "/steps/" + id + "/confirmation" }
	get := func(cookie *http.Cookie) onboarding.Plan {
		t.Helper()
		path := planURL
		if cookie == assigned {
			path = mentorInternURL + "/plan"
		}
		return decodePlan(t, apiRequest(t, h, "GET", path, "", cookie, 200))
	}
	set := func(cookie *http.Cookie, id string, confirmed bool, revision int64) onboarding.Plan {
		t.Helper()
		path := internURL(id)
		if cookie == assigned {
			path = mentorURL(id)
		}
		return decodePlan(t, apiRequest(t, h, "PUT", path, checkpointBody(confirmed, revision), cookie, 200))
	}
	plan := get(intern)
	initial := checkpointStep(t, plan, "step_intern")
	if initial.TemplateKey != "intern_task" || initial.Period == nil || *initial.Period != "1 неделя" ||
		initial.Instructions != "Инструкция" || len(initial.Resources) != 1 || initial.Resources[0].URL != nil ||
		!slices.Equal(initial.AllowedActions, []string{"confirm"}) {
		t.Fatal(initial)
	}
	confirm := checkpointBody(true, initial.Revision)
	for _, tc := range []struct {
		url, body string
		cookie    *http.Cookie
		status    int
		code      string
	}{
		{internURL("step_intern"), confirm, nil, 401, "UNAUTHENTICATED"},
		{internURL("step_mentor"), confirm, intern, 403, "ROLE_FORBIDDEN"},
		{internURL("step_intern"), confirm, assigned, 403, "ROLE_FORBIDDEN"},
		{mentorURL("step_both"), confirm, intern, 403, "ROLE_FORBIDDEN"},
		{mentorURL("step_intern"), confirm, assigned, 403, "ROLE_FORBIDDEN"},
		{mentorURL("step_both"), confirm, unassigned, 404, "NOT_FOUND"},
		{"/api/v1/mentor/interns/intern_demo_other/steps/step_local_setup_other/confirmation", confirm, assigned, 404, "NOT_FOUND"},
		{mentorURL("step_local_setup_2"), confirm, assigned, 404, "NOT_FOUND"},
		{internURL("step_both"), confirm, foreign, 404, "NOT_FOUND"},
		{internURL("step_dependent"), confirm, intern, 409, "STEP_TRANSITION_FORBIDDEN"},
		{internURL("step_local_setup"), confirm, intern, 409, "STEP_TRANSITION_FORBIDDEN"},
		{internURL("step_intern"), `{"confirmed":true,"revision":1,"user_id":"mentor_demo_1"}`, intern, 422, "VALIDATION_ERROR"},
		{internURL("step_intern"), `{"confirmed":true,"revision":0}`, intern, 422, "VALIDATION_ERROR"},
		{internURL("step_intern"), `{"confirmed":null,"revision":1}`, intern, 422, "VALIDATION_ERROR"},
		{internURL("step_intern"), `{"revision":1}`, intern, 422, "VALIDATION_ERROR"},
	} {
		assertCheckpointError(t, apiRequest(t, h, "PUT", tc.url, tc.body, tc.cookie, tc.status), tc.code)
	}
	r := httptest.NewRequest("PUT", mentorURL("step_both"), strings.NewReader(confirm))
	r.AddCookie(assigned)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	assertCheckpointError(t, w, "ORIGIN_FORBIDDEN")
	apiRequest(t, h, "PATCH", internStepsURL+"step_mentor", `{"status":"completed"}`, intern, 409)
	apiRequest(t, h, "PATCH", internStepsURL+"step_both", `{"status":"completed"}`, intern, 409)

	plan = set(intern, "step_intern", true, initial.Revision)
	internMark := checkpointStep(t, plan, "step_intern")
	if plan.ProgressPercent != 4 || internMark.Status != "completed" || len(internMark.Confirmations) != 1 ||
		internMark.Confirmations[0].UserID != "intern_demo_1" || internMark.Confirmations[0].Role != identity.Intern ||
		internMark.Confirmations[0].ConfirmedAt.IsZero() || internMark.Confirmations[0].ConfirmedAt.Location().String() != "UTC" {
		t.Fatal(plan)
	}
	if repeated := set(intern, "step_intern", true, initial.Revision); !reflect.DeepEqual(plan, repeated) {
		t.Fatal("idempotent confirm changed plan", repeated)
	}
	assertCheckpointError(t, apiRequest(t, h, "PUT", internURL("step_intern"), checkpointBody(false, initial.Revision), intern, 409),
		"CHECKPOINT_REVISION_CONFLICT")
	plan = decodePlan(t, apiRequest(t, h, "PATCH", internStepsURL+"step_intern", `{"status":"not_started"}`, intern, 200))
	if step := checkpointStep(t, plan, "step_intern"); step.Status != "not_started" || len(step.Confirmations) != 0 || plan.ProgressPercent != 0 {
		t.Fatal(plan)
	}
	plan = set(assigned, "step_mentor", true, plan.Steps[0].Revision)
	if step := checkpointStep(t, plan, "step_mentor"); step.Status != "completed" || plan.ProgressPercent != 4 ||
		len(step.Confirmations) != 1 || step.Confirmations[0].UserID != "mentor_demo_1" {
		t.Fatal(plan)
	}

	for _, tc := range []struct {
		id                                 string
		first, second                      *http.Cookie
		awaiting                           identity.Role
		partialProgress, completedProgress int
	}{
		{"step_both", intern, assigned, identity.Mentor, 4, 9},
		{"step_both_reverse", assigned, intern, identity.Intern, 9, 13},
	} {
		plan = set(tc.first, tc.id, true, plan.Steps[0].Revision)
		partial := checkpointStep(t, plan, tc.id)
		if partial.Status != "in_progress" || partial.AwaitingRole == nil || *partial.AwaitingRole != tc.awaiting ||
			len(partial.Confirmations) != 1 || !slices.Equal(partial.AllowedActions, []string{"revoke"}) ||
			plan.ProgressPercent != tc.partialProgress {
			t.Fatal(plan)
		}
		secondView := checkpointStep(t, get(tc.second), tc.id)
		if !slices.Equal(secondView.AllowedActions, []string{"confirm"}) {
			t.Fatal(secondView)
		}
		apiRequest(t, h, "PATCH", internStepsURL+tc.id, `{"status":"not_started"}`, intern, 409)
		plan = set(tc.second, tc.id, true, secondView.Revision)
		step := checkpointStep(t, plan, tc.id)
		if step.Status != "completed" || step.AwaitingRole != nil || len(step.Confirmations) != 2 ||
			step.Confirmations[0].Role != identity.Intern || step.Confirmations[1].Role != identity.Mentor ||
			plan.ProgressPercent != tc.completedProgress {
			t.Fatal(plan)
		}
	}
	plan = set(intern, "step_dependent", true, plan.Steps[0].Revision)
	plan = set(assigned, "step_dependent", true, plan.Steps[0].Revision)
	dependent := checkpointStep(t, plan, "step_dependent")
	both := checkpointStep(t, plan, "step_both")
	plan = set(intern, "step_both", false, both.Revision)
	revoked := checkpointStep(t, plan, "step_both")
	if plan.ProgressPercent != 13 || revoked.Status != "in_progress" || revoked.AwaitingRole == nil ||
		*revoked.AwaitingRole != identity.Intern || !reflect.DeepEqual(revoked.Confirmations, both.Confirmations[1:]) {
		t.Fatal(plan)
	}
	if repeated := set(intern, "step_both", false, both.Revision); !reflect.DeepEqual(plan, repeated) {
		t.Fatal("idempotent revoke changed plan", repeated)
	}
	if got := checkpointStep(t, plan, "step_dependent"); got.Status != "completed" || !reflect.DeepEqual(got.Confirmations, dependent.Confirmations) {
		t.Fatal("historical confirmations were lost", got)
	}
	plan = set(assigned, "step_dependent", false, plan.Steps[0].Revision)
	if got := checkpointStep(t, plan, "step_dependent"); got.Status != "blocked" ||
		!reflect.DeepEqual(got.Confirmations, dependent.Confirmations[:1]) {
		t.Fatal(got)
	}
	assertCheckpointError(t, apiRequest(t, h, "PUT", mentorURL("step_dependent"), checkpointBody(true, plan.Steps[0].Revision), assigned, 409),
		"STEP_TRANSITION_FORBIDDEN")

	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	h = requestHandler(db, Dependencies{})
	mentorPlan, internPlan := get(assigned), get(intern)
	if mentorPlan.ProgressPercent != 9 || internPlan.ProgressPercent != mentorPlan.ProgressPercent ||
		!reflect.DeepEqual(internPlan.NextStepID, mentorPlan.NextStepID) ||
		!reflect.DeepEqual(checkpointStep(t, internPlan, "step_both").Confirmations, revoked.Confirmations) {
		t.Fatal(internPlan, mentorPlan)
	}
	for _, step := range mentorPlan.Steps {
		if len(step.AllowedStatuses) != 0 {
			t.Fatal(step)
		}
	}
	w = apiRequest(t, h, "GET", "/api/v1/mentor/interns", "", assigned, 200)
	var list struct{ Items []mentor.InternSummary }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatal(w.Body.String(), err)
	}
	summary := list.Items[0]
	if summary.ProgressPercent != internPlan.ProgressPercent || summary.CompletedSteps != 2 ||
		summary.BlockedSteps != 1 || summary.TotalSteps != 22 || !reflect.DeepEqual(summary.NextStepID, internPlan.NextStepID) {
		t.Fatal(summary)
	}
}

func TestPostgresConcurrentCheckpointConfirmations(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,completion_policy)
VALUES ('step_concurrent','plan_demo_1',101,'Вместе','','not_started','both')`); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	intern := loginCookie(t, h, "intern_demo_1", nil)
	assigned := loginCookie(t, h, "mentor_demo_1", nil)
	type client struct {
		url    string
		cookie *http.Cookie
	}
	student := client{internStepsURL + "step_concurrent/confirmation", intern}
	teacher := client{mentorInternURL + "/steps/step_concurrent/confirmation", assigned}
	runPair := func(left, right client, revision int64) [2]*httptest.ResponseRecorder {
		t.Helper()
		var results [2]*httptest.ResponseRecorder
		start, done := make(chan struct{}), make(chan int, len(results))
		for i, caller := range []client{left, right} {
			go func() {
				<-start
				r := httptest.NewRequest("PUT", caller.url, strings.NewReader(checkpointBody(true, revision)))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", "http://localhost:5173")
				r.AddCookie(caller.cookie)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				results[i] = w
				done <- i
			}()
		}
		close(start)
		<-done
		<-done
		return results
	}
	plan := decodePlan(t, apiRequest(t, h, "GET", planURL, "", intern, 200))
	startRevision := checkpointStep(t, plan, "step_concurrent").Revision
	results := runPair(student, teacher, startRevision)
	loser, winner := 0, 1
	if results[0].Code == 200 {
		loser, winner = 1, 0
	}
	if results[winner].Code != 200 || results[loser].Code != 409 {
		t.Fatal(results[0].Code, results[0].Body.String(), results[1].Code, results[1].Body.String())
	}
	assertCheckpointError(t, results[loser], "CHECKPOINT_REVISION_CONFLICT")
	partial := checkpointStep(t, decodePlan(t, results[winner]), "step_concurrent")
	if partial.Revision != startRevision+1 || len(partial.Confirmations) != 1 || partial.Status != "in_progress" {
		t.Fatal(partial)
	}
	retry := []client{student, teacher}[loser]
	results = runPair(retry, retry, partial.Revision)
	var complete onboarding.Step
	for _, result := range results {
		if result.Code != 200 {
			t.Fatal(result.Code, result.Body.String())
		}
		step := checkpointStep(t, decodePlan(t, result), "step_concurrent")
		if step.Revision != partial.Revision+1 || len(step.Confirmations) != 2 || step.Status != "completed" {
			t.Fatal(step)
		}
		if complete.ID != "" && !reflect.DeepEqual(complete, step) {
			t.Fatal("duplicate changed confirmation or timestamp", complete, step)
		}
		complete = step
	}
}
