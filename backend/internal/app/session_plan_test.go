package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func apiRequest(t *testing.T, h http.Handler, method, path, body string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: want %d, got %d %s", method, path, want, w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	return w
}

type grantedAccess struct{}

func (grantedAccess) Check(context.Context, ports.AccessCheck) (bool, error) { return true, nil }

func TestPostgresPlanPrerequisitesAndEmptyPlan(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DemoMode: true, SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(cfg, db, logger, nil, grantedAccess{})
	cookie := loginCookie(t, h, "intern_demo_1", nil)
	if _, err := db.Exec(`INSERT INTO onboarding_steps(id,plan_id,position,title,description,status)
 VALUES('step_next','plan_demo_1',1,'Первая задача','Тестовый второй шаг','not_started');
 INSERT INTO onboarding_step_dependencies(plan_id,step_id,prerequisite_id) VALUES('plan_demo_1','step_next','step_local_setup')`); err != nil {
		t.Fatal(err)
	}
	plan := decodePlan(t, apiRequest(t, h, "GET", "/api/v1/onboarding/plan", "", cookie, 200))
	if len(plan.Steps) != 2 || plan.Steps[1].Status != "blocked" || plan.Steps[1].BlockedReason == nil {
		t.Fatal(plan)
	}
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_next", `{"status":"not_started"}`, cookie, 409)
	plan = decodePlan(t, apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, cookie, 200))
	if plan.ProgressPercent != 50 || plan.NextStepID == nil || *plan.NextStepID != "step_next" || plan.Steps[1].Status != "not_started" {
		t.Fatal(plan)
	}
	plan = decodePlan(t, apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_next", `{"status":"completed"}`, cookie, 200))
	if plan.ProgressPercent != 100 || plan.NextStepID != nil {
		t.Fatal(plan)
	}
	other := loginCookie(t, h, "intern_demo_other", nil)
	if _, err := db.Exec(`DELETE FROM onboarding_steps WHERE plan_id='plan_demo_other'`); err != nil {
		t.Fatal(err)
	}
	empty := decodePlan(t, apiRequest(t, h, "GET", "/api/v1/onboarding/plan", "", other, 200))
	if len(empty.Steps) != 0 || empty.NextStepID != nil || empty.ProgressPercent != 0 {
		t.Fatal(empty)
	}
	if _, err := db.Exec(`DELETE FROM onboarding_plans WHERE id='plan_demo_other'`); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "GET", "/api/v1/onboarding/plan", "", other, 404)
}
func loginCookie(t *testing.T, h http.Handler, id string, previous *http.Cookie) *http.Cookie {
	t.Helper()
	w := apiRequest(t, h, "POST", "/api/v1/demo/session", `{"account_id":"`+id+`"}`, previous, 200)
	return w.Result().Cookies()[0]
}
func decodePlan(t *testing.T, w *httptest.ResponseRecorder) onboarding.Plan {
	t.Helper()
	var plan onboarding.Plan
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Steps == nil {
		t.Fatal("null steps")
	}
	for _, step := range plan.Steps {
		if step.RequestIDs == nil || step.AllowedStatuses == nil {
			t.Fatal("null step arrays")
		}
	}
	return plan
}
func TestPostgresSessionPlanLifecycle(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(ctx, db, false); err == nil {
		t.Fatal("seed outside demo")
	}
	cfg := config.Config{DemoMode: true, SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(cfg, db, logger, nil, nil)
	accounts := apiRequest(t, h, "GET", "/api/v1/demo/accounts", "", nil, 200)
	var users []auth.User
	if err := json.Unmarshal(accounts.Body.Bytes(), &users); err != nil || len(users) != 4 {
		t.Fatal(err, users)
	}
	for _, path := range []string{"/api/v1/me", "/api/v1/onboarding/plan"} {
		apiRequest(t, h, "GET", path, "", nil, 401)
	}
	cookie := loginCookie(t, h, "intern_demo_1", nil)
	hash := sha256.Sum256([]byte(cookie.Value))
	var hashes int
	if err := db.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash=$1`, hash[:]).Scan(&hashes); err != nil || hashes != 1 {
		t.Fatal(err, hashes)
	}
	p := decodePlan(t, apiRequest(t, h, "GET", "/api/v1/onboarding/plan", "", cookie, 200))
	if p.ID != "plan_demo_1" || len(p.Steps) != 1 || len(p.Steps[0].RequestIDs) != 0 || p.NextStepID == nil || *p.NextStepID != "step_local_setup" {
		t.Fatal(p)
	}
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, cookie, 409)
	for _, body := range []string{`{}`, `{"status":null}`, `{"status":"blocked"}`, `{"status":"wat"}`, `{"status":"in_progress","user_id":"intern_demo_2"}`} {
		apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", body, cookie, 422)
	}
	p = decodePlan(t, apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"not_started"}`, cookie, 200))
	if p.Steps[0].Status != "not_started" {
		t.Fatal(p)
	}
	var revision int64
	if err := db.QueryRow(`SELECT revision FROM onboarding_plans WHERE id='plan_demo_1'`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"not_started"}`, cookie, 200)
	var repeated int64
	if err := db.QueryRow(`SELECT revision FROM onboarding_plans WHERE id='plan_demo_1'`).Scan(&repeated); err != nil || repeated != revision {
		t.Fatal("repeat changed revision", err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	restarted := NewHandler(cfg, db, logger, nil, nil)
	apiRequest(t, restarted, "GET", "/api/v1/me", "", cookie, 200)
	p = decodePlan(t, apiRequest(t, restarted, "GET", "/api/v1/onboarding/plan", "", cookie, 200))
	if p.Steps[0].Status != "not_started" {
		t.Fatal("seed or restart reset progress", p)
	}
	// Same team and another team cannot mutate the first user's step.
	for _, id := range []string{"intern_demo_2", "intern_demo_other"} {
		other := loginCookie(t, h, id, nil)
		apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"in_progress"}`, other, 404)
		theirPlan := decodePlan(t, apiRequest(t, h, "GET", "/api/v1/onboarding/plan", "", other, 200))
		if theirPlan.ID == p.ID {
			t.Fatal("foreign plan leaked")
		}
	}
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/missing", `{"status":"completed"}`, cookie, 404)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	apiRequest(t, h, "GET", "/api/v1/me", "", mentor, 200)
	apiRequest(t, h, "GET", "/api/v1/onboarding/plan", "", mentor, 403)
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, mentor, 403)
	replacement := loginCookie(t, h, "intern_demo_2", cookie)
	apiRequest(t, h, "GET", "/api/v1/me", "", cookie, 401)
	if _, err := db.Exec(`UPDATE sessions SET expires_at=now()-interval '1 second' WHERE user_id='intern_demo_2'`); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "GET", "/api/v1/me", "", replacement, 401)
	for range 2 {
		apiRequest(t, h, "DELETE", "/api/v1/session", "", replacement, 204)
	}
	live := loginCookie(t, h, "intern_demo_1", nil)
	apiRequest(t, h, "DELETE", "/api/v1/session", "", live, 204)
	apiRequest(t, h, "GET", "/api/v1/me", "", live, 401)
	// A non-demo account may never be listed or used by demo/session.
	if _, err := db.Exec(`INSERT INTO users(id,team_id,name,role) VALUES('private','team_demo_backend','Private','intern')`); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "POST", "/api/v1/demo/session", `{"account_id":"private"}`, nil, 404)
	valid := loginCookie(t, h, "intern_demo_1", nil)
	apiRequest(t, h, "POST", "/api/v1/demo/session", `{"account_id":"private"}`, valid, 404)
	apiRequest(t, h, "GET", "/api/v1/me", "", valid, 200)
	cfg.DemoMode = false
	production := NewHandler(cfg, db, logger, nil, nil)
	apiRequest(t, production, "GET", "/api/v1/demo/accounts", "", nil, 404)
	apiRequest(t, production, "POST", "/api/v1/demo/session", `{"account_id":"intern_demo_1"}`, nil, 404)
	apiRequest(t, production, "GET", "/api/v1/me", "", valid, 200)
	apiRequest(t, production, "GET", "/api/v1/onboarding/plan", "", valid, 503)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "GET", "/api/v1/me", "", valid, 503)
}
