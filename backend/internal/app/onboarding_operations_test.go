package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresMeetingsAndAgreements(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(context.Background(), db, true); err != nil {
		t.Fatal(err)
	}
	newHandler := func() http.Handler {
		return NewHandler(config.Config{DemoMode: true, SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"}}, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, Dependencies{})
	}
	h := newHandler()
	intern := loginCookie(t, h, "intern_demo_1", nil)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	other := loginCookie(t, h, "intern_demo_other", nil)
	hr := loginCookie(t, h, "hr_demo_1", nil)
	first := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	second := "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	topic := "cccccccc-cccc-4ccc-accc-cccccccccccc"
	mentorTopic := "dddddddd-dddd-4ddd-addd-dddddddddddd"
	path := "/api/v1/meetings"
	apiRequest(t, h, "GET", path, "", nil, 401)
	apiRequest(t, h, "POST", path, "{", intern, 400)
	for _, bad := range []string{
		`{"action":"create","id":"invalid"}`,
		`{"action":"create","id":"aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaax"}`,
		`{"action":"create","id":"aaaaaaaa_aaaa-4aaa-aaaa-aaaaaaaaaaaa"}`,
		`{"action":"unknown","id":"aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"}`,
		`{"action":"complete","id":"aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa","content":" "}`,
		`{"action":"complete","id":"aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa","content":"Итог","agreements":[{"content":""}]}`,
	} {
		apiRequest(t, h, "POST", path, bad, intern, 422)
	}
	create := func(id string) string {
		return `{"action":"create","id":"` + id + `","intern_id":"intern_demo_1","scheduled_at":"2030-04-15T12:00:00Z","timezone":"Europe/Moscow"}`
	}
	apiRequest(t, h, "POST", path, create(first), intern, 200)
	apiRequest(t, h, "POST", path, create(first), intern, 200)
	apiRequest(t, h, "POST", path, strings.Replace(create(first), "Europe/Moscow", "UTC", 1), intern, 409)
	apiRequest(t, h, "POST", path, create(first), other, 404)
	apiRequest(t, h, "POST", path, create(first), hr, 404)
	apiRequest(t, h, "POST", path, `{"action":"topic","id":"`+first+`","topic_id":"`+topic+`","revision":1,"content":"Закрытая заметка","private":true}`, intern, 200)
	apiRequest(t, h, "POST", path, `{"action":"topic","id":"`+first+`","topic_id":"`+topic+`","revision":2,"content":"Подмена"}`, mentor, 404)
	apiRequest(t, h, "POST", path, `{"action":"topic","id":"`+first+`","topic_id":"`+mentorTopic+`","revision":2,"content":"Общая тема"}`, mentor, 200)
	apiRequest(t, h, "POST", path, `{"action":"cancel","id":"`+first+`","revision":2}`, mentor, 409)
	for _, cookie := range []*http.Cookie{other, hr} {
		w := apiRequest(t, h, "GET", path, "", cookie, 200)
		if strings.Contains(w.Body.String(), first) {
			t.Fatal("foreign meeting disclosed")
		}
	}
	w := apiRequest(t, h, "GET", path, "", mentor, 200)
	if !strings.Contains(w.Body.String(), "Общая тема") || strings.Contains(w.Body.String(), "Закрытая заметка") {
		t.Fatal(w.Body.String())
	}
	apiRequest(t, h, "POST", path, `{"action":"reschedule","id":"`+first+`","revision":3,"timezone":"UTC","scheduled_at":"2030-04-16T09:00:00Z"}`, mentor, 200)
	apiRequest(t, h, "POST", path, `{"action":"complete","id":"`+first+`","revision":4,"content":"Итоги","agreements":[{"owner_id":"hr_demo_1","content":"Недопустимо"}]}`, mentor, 422)
	finish := `{"action":"complete","id":"` + first + `","revision":4,"content":"Итоги","agreements":[{"owner_id":"intern_demo_1","content":"Продолжить работу","step_id":"step_local_setup"}]}`
	apiRequest(t, h, "POST", path, strings.Replace(finish, "step_local_setup", "step_local_setup_2", 1), mentor, 422)
	if _, err := db.Exec("ALTER TABLE onboarding_agreements ADD CONSTRAINT test_reject_agreement CHECK (content <> 'Продолжить работу')"); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "POST", path, finish, mentor, 503)
	if _, err := db.Exec("ALTER TABLE onboarding_agreements DROP CONSTRAINT test_reject_agreement"); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "POST", path, finish, mentor, 200)
	apiRequest(t, h, "POST", path, finish, mentor, 200)
	apiRequest(t, h, "POST", path, strings.Replace(finish, "Итоги", "Другие итоги", 1), mentor, 409)
	apiRequest(t, h, "POST", path, create(second), mentor, 200)
	h = newHandler()
	w = apiRequest(t, h, "GET", path, "", intern, 200)
	var result struct {
		Meetings []struct {
			ID         string
			Previous   *string `json:"previous_id"`
			Agreements []struct{ ID string }
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var agreementID string
	for _, meeting := range result.Meetings {
		if meeting.ID == first {
			if len(meeting.Agreements) != 1 {
				t.Fatal(w.Body.String())
			}
			agreementID = meeting.Agreements[0].ID
		}
		if meeting.ID == second && (meeting.Previous == nil || *meeting.Previous != first) {
			t.Fatal(w.Body.String())
		}
	}
	if strings.Count(w.Body.String(), "Продолжить работу") != 2 {
		t.Fatal(w.Body.String())
	}
	complete := `{"action":"agreement-complete","id":"` + agreementID + `","revision":1}`
	apiRequest(t, h, "POST", path, complete, mentor, 404)
	apiRequest(t, h, "POST", path, `{"action":"agreement-reschedule","id":"`+agreementID+`","revision":1,"due_at":"2030-04-23T09:00:00Z"}`, intern, 200)
	apiRequest(t, h, "POST", path, complete, intern, 409)
	apiRequest(t, h, "POST", path, strings.Replace(complete, `"revision":1`, `"revision":2`, 1), intern, 200)
	apiRequest(t, h, "POST", path, strings.Replace(complete, `"revision":1`, `"revision":2`, 1), intern, 200)
	if w = apiRequest(t, h, "GET", path, "", intern, 200); strings.Count(w.Body.String(), "Продолжить работу") != 1 {
		t.Fatal(w.Body.String())
	}
	apiRequest(t, h, "POST", path, `{"action":"cancel","id":"`+second+`","revision":1}`, mentor, 200)
	if _, err := db.Exec("UPDATE mentor_assignments SET mentor_id='mentor_demo_2' WHERE intern_id='intern_demo_1'"); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range []*http.Cookie{mentor, intern, loginCookie(t, h, "mentor_demo_2", nil)} {
		if w = apiRequest(t, h, "GET", path, "", cookie, 200); strings.Contains(w.Body.String(), first) {
			t.Fatal("revoked meeting disclosed")
		}
	}
	apiRequest(t, h, "POST", path, finish, mentor, 404)
	if _, err := db.Exec("ALTER TABLE onboarding_meetings RENAME TO temporarily_unavailable_meetings"); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "GET", path, "", intern, 503)
}

func TestPostgresCuratorScopeProblemsAndTemplate(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(context.Background(), db, true); err != nil {
		t.Fatal(err)
	}
	newHandler := func() http.Handler {
		return NewHandler(config.Config{DemoMode: true, SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"}}, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, Dependencies{})
	}
	h := newHandler()
	curator := loginCookie(t, h, "mentor_demo_1", nil)
	stranger := loginCookie(t, h, "mentor_demo_2", nil)
	path := "/api/v1/curator"
	full := path + "?team_id=team_demo_backend"
	apiRequest(t, h, "GET", path, "", nil, 401)
	apiRequest(t, h, "GET", full, "", stranger, 404)
	apiRequest(t, h, "GET", path+"?team_id=team_demo_frontend", "", curator, 404)
	apiRequest(t, h, "GET", full+"&from=bad", "", curator, 422)
	apiRequest(t, h, "GET", full+"&kind=unknown", "", curator, 422)
	apiRequest(t, h, "POST", path, "{", curator, 400)
	for _, bad := range []string{
		`{}`,
		`{"action":"publish-template","team_id":"team_demo_backend","id":"eeeeeeee_eeee-4eee-aeee-eeeeeeeeeeee"}`,
		`{"action":"publish-template","team_id":"team_demo_backend","id":"eeeeeeee-eeee-4eee-aeee-eeeeeeeeeeex"}`,
		`{"action":"publish-template","team_id":"team_demo_backend","id":"eeeeeeee-eeee-4eee-aeee-eeeeeeeeeeee","steps":[]}`,
		`{"action":"publish-template","team_id":"team_demo_backend","id":"eeeeeeee-eeee-4eee-aeee-eeeeeeeeeeee","steps":[{"key":"a","title":"A","description":"A"},{"key":"a","title":"A","description":"A"}]}`,
		`{"action":"invalid","team_id":"team_demo_backend","id":"eeeeeeee-eeee-4eee-aeee-eeeeeeeeeeee"}`,
		`{"action":"save-problem","team_id":"team_demo_backend","id":"eeeeeeee-eeee-4eee-aeee-eeeeeeeeeeee","kind":"blocked","direction":"mentor","title":"Блокер","description":"Два события","status":"resolved"}`,
	} {
		apiRequest(t, h, "POST", path, bad, curator, 422)
	}
	if w := apiRequest(t, h, "GET", path, "", curator, 200); !strings.Contains(w.Body.String(), "team_demo_backend") {
		t.Fatal(w.Body.String())
	}
	if _, err := db.Exec(`
 UPDATE onboarding_steps SET status='completed' WHERE id='step_local_setup';
 INSERT INTO onboarding_step_confirmations(step_id,role,user_id) VALUES ('step_local_setup','intern','intern_demo_1') ON CONFLICT DO NOTHING;
 INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,blocked_reason,template_key)
 VALUES ('blocked-test-1','plan_demo_1',101,'Один блокер','Тест','blocked','Ожидание','common-block'),
 ('blocked-test-2','plan_demo_2',101,'Один блокер','Тест','blocked','Ожидание','common-block');
 UPDATE onboarding_steps SET meeting_changed_at=now()-interval '5 hours' WHERE id='blocked-test-1';
 UPDATE onboarding_steps SET meeting_changed_at=now()-interval '3 hours' WHERE id='blocked-test-2'`); err != nil {
		t.Fatal(err)
	}
	var workspace struct {
		Summaries []struct {
			Topic           string
			Interns, Events int
			Average         float64 `json:"average_hours"`
		}
	}
	w := apiRequest(t, h, "GET", full+"&kind=blocked", "", curator, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &workspace); err != nil {
		t.Fatal(err)
	}
	if len(workspace.Summaries) != 1 || workspace.Summaries[0].Interns != 2 || workspace.Summaries[0].Events != 2 || workspace.Summaries[0].Average != 4 {
		t.Fatal(workspace)
	}
	template := `{"action":"publish-template","id":"eeeeeeee-eeee-4eee-aeee-eeeeeeeeeeee","team_id":"team_demo_backend","revision":0,"existing_plans":true,"steps":[{"key":"new-template-step","title":"Новый шаг","description":"Инструкция"}]}`
	apiRequest(t, h, "POST", path, template, stranger, 404)
	apiRequest(t, h, "POST", path, template, curator, 200)
	apiRequest(t, h, "POST", path, template, curator, 200)
	apiRequest(t, h, "POST", path, strings.Replace(template, "eeeeeeee", "faaaaaaa", 1), curator, 409)
	apiRequest(t, h, "POST", path, strings.Replace(template, "Инструкция", "Другой текст", 1), curator, 409)
	if _, err := db.Exec("ALTER TABLE onboarding_steps ADD CONSTRAINT test_reject_template CHECK (title <> 'Ошибка шаблона')"); err != nil {
		t.Fatal(err)
	}
	next := strings.ReplaceAll(strings.ReplaceAll(template, "eeeeeeee-eeee", "dddddddd-dddd"), `"revision":0`, `"revision":1`)
	apiRequest(t, h, "POST", path, strings.Replace(next, "Новый шаг", "Ошибка шаблона", 1), curator, 503)
	if _, err := db.Exec("ALTER TABLE onboarding_steps DROP CONSTRAINT test_reject_template"); err != nil {
		t.Fatal(err)
	}
	var published int
	if err := db.QueryRow("SELECT count(*) FROM onboarding_template_publications").Scan(&published); err != nil || published != 1 {
		t.Fatal(published, err)
	}
	var completed string
	var count int
	if err := db.QueryRow("SELECT status FROM onboarding_steps WHERE id='step_local_setup'").Scan(&completed); err != nil || completed != "completed" {
		t.Fatal(completed, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM onboarding_steps WHERE plan_id='plan_demo_1' AND template_key='new-template-step'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := db.Exec("INSERT INTO users(id,team_id,name,role) VALUES('new-intern-test','team_demo_backend','Новый','intern'); INSERT INTO onboarding_plans(id,user_id,team_id,title) VALUES('new-plan-test','new-intern-test','team_demo_backend','План')"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM onboarding_steps WHERE plan_id='new-plan-test' AND template_key='new-template-step'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	id := "ffffffff-ffff-4fff-afff-ffffffffffff"
	other := "aaaaaaaa-cccc-4aaa-aaaa-aaaaaaaaaaaa"
	create := func(id string) string {
		return `{"action":"save-problem","id":"` + id + `","team_id":"team_demo_backend","revision":0,"kind":"blocked","direction":"mentor","status":"open","title":"Блокер","description":"Две записи","evidence":[{"kind":"blocked","id":"blocked-test-1"},{"kind":"blocked","id":"blocked-test-1"}]}`
	}
	apiRequest(t, h, "POST", path, create(id), curator, 200)
	apiRequest(t, h, "POST", path, create(id), curator, 409)
	apiRequest(t, h, "POST", path, create(other), curator, 200)
	update := `{"action":"save-problem","id":"` + id + `","team_id":"team_demo_backend","revision":1,"kind":"blocked","direction":"mentor","status":"resolved","title":"Решено","description":"Доказательства","resolution":"Опубликован шаблон","template_version":1,"owner_id":"mentor_demo_1","merge_from":"` + other + `","merge_revision":1}`
	apiRequest(t, h, "POST", path, strings.Replace(update, `"owner_id":"mentor_demo_1"`, `"owner_id":"hr_demo_1"`, 1), curator, 422)
	apiRequest(t, h, "POST", path, strings.Replace(update, `"template_version":1`, `"template_version":99`, 1), curator, 404)
	apiRequest(t, h, "POST", path, strings.Replace(update, `"template_version":1`, `"document_id":"foreign-document"`, 1), curator, 404)
	apiRequest(t, h, "POST", path, strings.Replace(update, other, id, 1), curator, 422)
	apiRequest(t, h, "POST", path, strings.Replace(update, `"merge_revision":1`, `"merge_revision":2`, 1), curator, 409)
	apiRequest(t, h, "POST", path, strings.Replace(create("bbbbbbbb-cccc-4aaa-aaaa-aaaaaaaaaaaa"), "blocked-test-1", "foreign-step", 1), curator, 422)
	apiRequest(t, h, "POST", path, strings.Replace(create("bbbbbbbb-cccc-4aaa-aaaa-aaaaaaaaaaaa"), `"kind":"blocked","id"`, `"kind":"access","id"`, 1), curator, 422)
	apiRequest(t, h, "POST", path, update, curator, 200)
	apiRequest(t, h, "POST", path, update, curator, 409)
	h = newHandler()
	w = apiRequest(t, h, "GET", full+"&status=resolved", "", curator, 200)
	if !strings.Contains(w.Body.String(), "Опубликован шаблон") || !strings.Contains(w.Body.String(), "blocked-test-1") || strings.Count(w.Body.String(), "\"snapshot\"") != 2 {
		t.Fatal(w.Body.String())
	}
	if _, err := db.Exec("DELETE FROM curator_team_assignments WHERE curator_id='mentor_demo_1'"); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "GET", full, "", curator, 404)
	apiRequest(t, h, "POST", path, update, curator, 404)
	if _, err := db.Exec("ALTER TABLE curator_team_assignments RENAME TO temporarily_unavailable_scope"); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "GET", path, "", curator, 503)
}
