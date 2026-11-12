package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/mentor"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
	"github.com/jackc/pgx/v5/pgconn"
)

const availableURL = "/api/v1/mentor/available-interns"
const freeIntern = "intern_demo_unassigned"

func assignmentURL(id string) string { return "/api/v1/mentor/interns/" + id + "/assignment" }

type assignmentFixture struct {
	db                     *sql.DB
	h                      http.Handler
	first, second, student *http.Cookie
}

func newAssignmentFixture(t *testing.T) assignmentFixture {
	t.Helper()
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	// #44 owns shared demo seed. #43 deliberately uses isolated test data.
	if _, err := db.Exec(`INSERT INTO users(id,name,team_id,role,demo_account) VALUES
 ('intern_demo_unassigned','Свободный стажёр','team_demo_backend','intern',true),
 ('intern_free_second','Свободный стажёр','team_demo_backend','intern',true)`); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	return assignmentFixture{db, h, loginCookie(t, h, "mentor_demo_1", nil), loginCookie(t, h, "mentor_demo_2", nil), loginCookie(t, h, freeIntern, nil)}
}

// A request without Content-Type is intentional: the PUT has no request body.
func assignHTTP(ctx context.Context, h http.Handler, id, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", assignmentURL(id), strings.NewReader(body)).WithContext(ctx)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func assertAssignmentStatus(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("HTTP %d, want %d: %s", w.Code, status, w.Body.String())
	}
	if code != "" {
		// Validate the standard error shape without relying on message wording.
		var envelope map[string]map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope["error"]["code"] != code || envelope["error"]["trace_id"] == "" || envelope["error"]["trace_id"] == nil {
			t.Fatal(w.Body.String())
		}
	}
}
func readAvailable(t *testing.T, f assignmentFixture) []mentor.AvailableIntern {
	t.Helper()
	w := apiRequest(t, f.h, "GET", availableURL, "", f.first, 200)
	var list struct {
		Items []mentor.AvailableIntern `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Items == nil {
		t.Fatalf("invalid list: %v %s", err, w.Body.String())
	}
	return list.Items
}

func TestPostgresMentorAssignmentLifecycle(t *testing.T) {
	f := newAssignmentFixture(t)
	candidates := readAvailable(t, f)
	if len(candidates) != 2 || candidates[0].ID != freeIntern || candidates[1].ID != "intern_free_second" {
		t.Fatal(candidates)
	}
	for _, c := range candidates {
		if c.Team.ID != "team_demo_backend" {
			t.Fatal(c)
		}
	}
	response := assignHTTP(t.Context(), f.h, freeIntern, "", f.first, "http://localhost:5173")
	assertAssignmentStatus(t, response, 200, "")
	var assignment mentor.MentorAssignment
	if err := json.Unmarshal(response.Body.Bytes(), &assignment); err != nil || assignment != (mentor.MentorAssignment{InternID: freeIntern, MentorID: "mentor_demo_1", TeamID: "team_demo_backend"}) {
		t.Fatal(assignment, err)
	}
	if left := readAvailable(t, f); len(left) != 1 || left[0].ID != "intern_free_second" {
		t.Fatal(left)
	}
	// Dropping the response is harmless: a fresh handler/list verifies persistence.
	f.h = requestHandler(f.db, Dependencies{})
	var list struct {
		Items []mentor.InternSummary `json:"items"`
	}
	result := apiRequest(t, f.h, "GET", "/api/v1/mentor/interns", "", f.first, 200)
	if err := json.Unmarshal(result.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range list.Items {
		if i.ID == freeIntern {
			found = true
			if i.TotalSteps != 0 || i.ProgressPercent != 0 || i.NextStepID != nil {
				t.Fatal(i)
			}
		}
	}
	if !found {
		t.Fatal("assignment missing from list", list)
	}
	apiRequest(t, f.h, "GET", "/api/v1/mentor/interns/"+freeIntern+"/plan", "", f.first, 404)
	var plans int
	if err := f.db.QueryRow(`SELECT count(*) FROM onboarding_plans WHERE user_id=$1`, freeIntern).Scan(&plans); err != nil || plans != 0 {
		t.Fatal("assignment created a plan", plans, err)
	}
	repeat := assignHTTP(t.Context(), f.h, freeIntern, "", f.first, "http://localhost:5173")
	assertAssignmentStatus(t, repeat, 200, "")
	if repeat.Body.String() != response.Body.String() {
		t.Fatal("retry changed assignment", repeat.Body.String())
	}
	// Seed must not erase a manual assignment.
	if err := demo.Seed(t.Context(), f.db, true); err != nil {
		t.Fatal(err)
	}
	assertAssignmentStatus(t, assignHTTP(t.Context(), f.h, freeIntern, "", f.second, "http://localhost:5173"), 409, "INTERN_ALREADY_ASSIGNED")
	assertAssignmentStatus(t, assignHTTP(t.Context(), f.h, "intern_free_second", "", f.first, "http://localhost:5173"), 200, "")
	if left := readAvailable(t, f); len(left) != 0 {
		t.Fatal(left)
	}
	// A plan added independently later becomes available without re-assignment.
	if _, err := f.db.Exec(`INSERT INTO onboarding_plans(id,user_id,team_id,title) VALUES('plan_demo_unassigned',$1,'team_demo_backend','План')`, freeIntern); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, f.h, "GET", "/api/v1/mentor/interns/"+freeIntern+"/plan", "", f.first, 200)
}

func TestPostgresMentorAssignmentRejectsUnauthorizedAndBody(t *testing.T) {
	f := newAssignmentFixture(t)
	for _, test := range []struct {
		name, id, body, origin string
		cookie                 *http.Cookie
		status                 int
		code                   string
	}{
		{"anonymous", freeIntern, "", "http://localhost:5173", nil, 401, "UNAUTHENTICATED"},
		{"intern", freeIntern, "", "http://localhost:5173", f.student, 403, "ROLE_FORBIDDEN"},
		{"origin", freeIntern, "", "https://evil.invalid", f.first, 403, "ORIGIN_FORBIDDEN"},
		{"missing", "missing", "", "http://localhost:5173", f.first, 404, "NOT_FOUND"},
		{"other team", "intern_demo_other", "", "http://localhost:5173", f.first, 404, "NOT_FOUND"},
		{"mentor target", "mentor_demo_2", "", "http://localhost:5173", f.first, 404, "NOT_FOUND"},
		{"occupied", "intern_demo_2", "", "http://localhost:5173", f.first, 409, "INTERN_ALREADY_ASSIGNED"},
		{"object body", freeIntern, "{}", "http://localhost:5173", f.first, 422, "VALIDATION_ERROR"},
		{"principal injection", freeIntern, `{"mentor_id":"mentor_demo_2","team_id":"team_demo_frontend"}`, "http://localhost:5173", f.first, 422, "VALIDATION_ERROR"},
		{"whitespace", freeIntern, " ", "http://localhost:5173", f.first, 422, "VALIDATION_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := assignHTTP(t.Context(), f.h, test.id, test.body, test.cookie, test.origin)
			assertAssignmentStatus(t, w, test.status, test.code)
			if strings.Contains(w.Body.String(), "mentor_demo_2") {
				t.Fatal("error discloses another mentor", w.Body.String())
			}
		})
	}
	apiRequest(t, f.h, "GET", availableURL, "", nil, 401)
	apiRequest(t, f.h, "GET", availableURL, "", f.student, 403)
	if candidates := readAvailable(t, f); len(candidates) != 2 {
		t.Fatal("failed request assigned a user", candidates)
	}
}

func waitAssignmentLocks(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
   AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%FOR SHARE OF u,m%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("expected %d concurrent requests, got %d", want, n)
		case <-ticker.C:
		}
	}
}

func TestPostgresMentorAssignmentConcurrent(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(map[bool]string{false: "different mentors", true: "same mentor"}[same], func(t *testing.T) {
			f := newAssignmentFixture(t)
			lock, err := f.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback()
			if _, err = lock.Exec(`SELECT id FROM users WHERE id=$1 FOR UPDATE`, freeIntern); err != nil {
				t.Fatal(err)
			}
			second := f.second
			if same {
				second = f.first
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			results := make(chan *httptest.ResponseRecorder, 2)
			for _, cookie := range []*http.Cookie{f.first, second} {
				go func() { results <- assignHTTP(ctx, f.h, freeIntern, "", cookie, "http://localhost:5173") }()
			}
			waitAssignmentLocks(t, f.db, 2)
			if err = lock.Commit(); err != nil {
				t.Fatal(err)
			}
			successes, conflicts := 0, 0
			var winner mentor.MentorAssignment
			for range 2 {
				select {
				case w := <-results:
					if w.Code == 200 {
						successes++
						var result mentor.MentorAssignment
						if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						if winner.InternID != "" && winner != result {
							t.Fatal("same operation returned different assignments")
						}
						winner = result
					} else {
						assertAssignmentStatus(t, w, 409, "INTERN_ALREADY_ASSIGNED")
						conflicts++
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			expected := 1
			if same {
				expected = 2
			}
			if successes != expected || conflicts != 2-expected {
				t.Fatal(successes, conflicts)
			}
			var count int
			var owner string
			if err = f.db.QueryRow(`SELECT count(*),min(mentor_id) FROM mentor_assignments WHERE intern_id=$1`, freeIntern).Scan(&count, &owner); err != nil || count != 1 || owner != winner.MentorID {
				t.Fatal(count, owner, winner, err)
			}
		})
	}
}

func TestPostgresAssignedInternTxRetainsLocks(t *testing.T) {
	f := newAssignmentFixture(t)
	assertAssignmentStatus(t, assignHTTP(t.Context(), f.h, freeIntern, "", f.first, "http://localhost:5173"), 200, "")
	principal := identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor}
	ctx := identity.WithPrincipal(t.Context(), principal)
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Nil Store.DB proves the port uses only the supplied transaction.
	owner, err := (mentor.Store{}).AssignedInternTx(ctx, tx, freeIntern)
	if err != nil || owner != (identity.Principal{UserID: freeIntern, TeamID: principal.TeamID, Role: identity.Intern}) {
		t.Fatal(owner, err)
	}
	for _, statement := range []string{
		`DELETE FROM mentor_assignments WHERE intern_id='intern_demo_unassigned'`,
		`UPDATE users SET role='mentor' WHERE id='intern_demo_unassigned'`,
		`UPDATE users SET role='intern' WHERE id='mentor_demo_1'`,
	} {
		contender, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = contender.Exec(`SET LOCAL lock_timeout='150ms'`); err != nil {
			contender.Rollback()
			t.Fatal(err)
		}
		_, err = contender.Exec(statement)
		contender.Rollback()
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "55P03" {
			t.Fatalf("assignment/roles were not locked: %v", err)
		}
	}
	if _, err = (mentor.Store{}).AssignedInternTx(ctx, tx, "intern_demo_2"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = (mentor.Store{}).AssignedInternTx(t.Context(), tx, freeIntern); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err = (mentor.Store{}).AssignedInternTx(identity.WithPrincipal(ctx, owner), tx, freeIntern); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal("port completed caller transaction", err)
	}
	if _, err = f.db.Exec(`DELETE FROM mentor_assignments WHERE intern_id=$1`, freeIntern); err != nil {
		t.Fatal(err)
	}
	tx, err = f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = (mentor.Store{}).AssignedInternTx(ctx, tx, freeIntern); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPostgresMentorAssignmentRoutesNewKnowledgeGap(t *testing.T) {
	f := newAssignmentFixture(t)
	_, student, conversation := prepareDraft(t, f.db)
	var message string
	if err := f.db.QueryRow(`UPDATE chat_messages SET outcome='knowledge_gap' WHERE conversation_id=$1 AND role='assistant' RETURNING id`, conversation).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM mentor_assignments WHERE intern_id='intern_demo_1'`); err != nil {
		t.Fatal(err)
	}
	url := "/api/v1/messages/" + message + "/knowledge-gap"
	apiRequest(t, f.h, "POST", url, "", student, 409)
	assertAssignmentStatus(t, assignHTTP(t.Context(), f.h, "intern_demo_1", "", f.second, "http://localhost:5173"), 200, "")
	response := apiRequest(t, f.h, "POST", url, "", student, 201)
	var q knowledgegaps.Status
	if err := json.Unmarshal(response.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, f.h, "GET", "/api/v1/mentor/questions/"+q.ID, "", f.first, 404)
	apiRequest(t, f.h, "GET", "/api/v1/mentor/questions/"+q.ID, "", f.second, 200)
	repeated := apiRequest(t, requestHandler(f.db, Dependencies{}), "GET", url, "", student, 200)
	var current struct {
		Question knowledgegaps.Status `json:"question"`
	}
	if err := json.Unmarshal(repeated.Body.Bytes(), &current); err != nil || !reflect.DeepEqual(q, current.Question) {
		t.Fatal(current, err)
	}
}
