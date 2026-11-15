package app

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresMeetingInvitationFromChat(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	intern := loginCookie(t, h, "intern_demo_1", nil)
	mentor := loginCookie(t, h, "mentor_demo_1", nil)
	foreign := loginCookie(t, h, "mentor_demo_2", nil)
	at := time.Now().UTC().AddDate(0, 0, 3).Truncate(time.Minute)
	date := at.Format("2006-01-02")
	var conv chat.Conversation
	created := apiRequest(t, h, "POST", "/api/v1/conversations", fmt.Sprintf(`{"client_conversation_id":%q,"step_id":"step_local_setup"}`, hrKey(8081)), intern, 201)
	if err := json.Unmarshal(created.Body.Bytes(), &conv); err != nil || conv.ID == "" {
		t.Fatal(err, created.Body.String())
	}
	var seq int
	ask := func(text string) string {
		t.Helper()
		seq++
		body, _ := json.Marshal(map[string]string{"client_message_id": hrKey(8090 + seq), "text": text})
		r := httptest.NewRequest("POST", "/api/v1/conversations/"+conv.ID+"/messages", strings.NewReader(string(body)))
		r.AddCookie(intern)
		r.Header.Set("Origin", "http://localhost:5173")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "event: message.completed") {
			t.Fatal(w.Code, w.Body.String())
		}
		var id string
		if err := db.QueryRow(`SELECT id FROM chat_messages WHERE conversation_id=$1 AND role='assistant' ORDER BY sequence DESC LIMIT 1`, conv.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	type draft struct {
		ID       string   `json:"id"`
		Mentor   string   `json:"mentor_id"`
		Existing string   `json:"existing_id"`
		Time     string   `json:"time"`
		Date     string   `json:"date"`
		Zone     string   `json:"timezone"`
		Title    string   `json:"title"`
		Reason   string   `json:"reason"`
		Topics   []string `json:"topics"`
		Missing  []string `json:"missing"`
	}
	type suggestion struct {
		Proposal *draft `json:"proposal"`
	}
	suggest := func(text string) *draft {
		t.Helper()
		return hrResponse[suggestion](t, h, "GET", "/meetings/suggestion?message_id="+ask(text), nil, intern, 200).Proposal
	}
	if p := suggest("Как запустить проект?"); p != nil {
		t.Fatal(p)
	}
	if p := suggest("Не могу продолжить из-за блокера"); p != nil {
		t.Fatal(p)
	}
	if p := suggest("Создай встречу с Аней в 15:00 или 16:00"); p == nil || p.Mentor != "" || p.Time != "" || len(p.Missing) < 2 {
		t.Fatal(p)
	}
	for text, reason := range map[string]string{"Нужно согласовать приоритеты": "priorities", "Хочу обратную связь по задаче": "feedback"} {
		if p := suggest(text); p == nil || p.Reason != reason {
			t.Fatal(text, p)
		}
	}
	id := ask("Создай встречу с моим наставником " + date + " в 15:00 по Москве. Тема — цели на месяц. В повестку добавь первую задачу и критерии успеха")
	apiRequest(t, h, "GET", "/api/v1/meetings/suggestion?message_id="+id, "", foreign, 404)
	p := hrResponse[suggestion](t, h, "GET", "/meetings/suggestion?message_id="+id, nil, intern, 200).Proposal
	if p == nil || p.Mentor != "mentor_demo_1" || p.Zone != "Europe/Moscow" || p.Time != "15:00" || p.Date != date ||
		p.Title != "цели на месяц" || len(p.Topics) != 2 || len(p.Missing) != 0 {
		t.Fatalf("proposal: %+v", p)
	}
	var question string
	if err := db.QueryRow(`INSERT INTO knowledge_gaps(message_id,student_id,team_id,mentor_id,step_id,direction,original_direction,question,context_excerpt)
  VALUES($1,'intern_demo_1','team_demo_backend','mentor_demo_1','step_local_setup','mentor','mentor','Получение ноутбука','') RETURNING id::text`, id).Scan(&question); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE onboarding_steps SET status='blocked',blocked_reason='Ноутбук ещё не выдан' WHERE id='step_local_setup'`); err != nil {
		t.Fatal(err)
	}
	if p := suggest("Не могу продолжить из-за блокера"); p == nil || p.Reason != "blocker" {
		t.Fatal(p)
	}
	path := "/api/v1/meetings"
	if body := apiRequest(t, h, "GET", path, "", intern, 200).Body.String(); strings.Contains(body, p.ID) || !strings.Contains(body, question) {
		t.Fatal(body)
	}
	payload := map[string]any{"id": p.ID, "action": "invite", "mentor_id": p.Mentor, "title": p.Title, "local_at": p.Date + "T" + p.Time,
		"timezone": p.Zone, "topics": p.Topics, "call_url": "https://example.com/call",
		"agenda_choices": []map[string]any{{"kind": "question", "id": question, "included": true}}}
	hrResponse[any](t, h, "POST", "/meetings", payload, foreign, 404)
	payload["mentor_id"] = "mentor_demo_2"
	hrResponse[any](t, h, "POST", "/meetings", payload, intern, 404)
	payload["mentor_id"] = p.Mentor
	payload["call_url"] = "javascript:alert(1)"
	hrResponse[any](t, h, "POST", "/meetings", payload, intern, 422)
	payload["call_url"] = "https://example.com/call"
	hrResponse[any](t, h, "POST", "/meetings", payload, intern, 200)
	hrResponse[any](t, h, "POST", "/meetings", payload, intern, 200)
	calendar := path + "/" + p.ID + "/calendar"
	apiRequest(t, h, "GET", calendar, "", intern, 404)
	mutate := func(action string, revision int) string {
		return fmt.Sprintf(`{"id":%q,"action":%q,"revision":%d}`, p.ID, action, revision)
	}
	apiRequest(t, h, "POST", path, mutate("accept", 1), intern, 404)
	apiRequest(t, h, "POST", path, mutate("accept", 1), foreign, 404)
	if body := apiRequest(t, h, "GET", path, "", foreign, 200).Body.String(); strings.Contains(body, p.ID) {
		t.Fatal(body)
	}
	if body := apiRequest(t, h, "GET", path, "", mentor, 200).Body.String(); !strings.Contains(body, `"status":"pending"`) || !strings.Contains(body, "критерии успеха") {
		t.Fatal(body)
	}
	if suggestion := suggest("Хочу обсудить с наставником"); suggestion == nil || suggestion.Existing != p.ID {
		t.Fatal(suggestion)
	}
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{
		"action": "agenda", "id": p.ID, "revision": 1, "agenda_choices": []map[string]any{{"kind": "question", "id": question, "included": false}},
	}, intern, 200)
	apiRequest(t, h, "POST", path, mutate("accept", 1), mentor, 409)
	if body := apiRequest(t, h, "GET", path, "", intern, 200).Body.String(); !strings.Contains(body, `"included":false`) || !strings.Contains(body, question) {
		t.Fatal(body)
	}
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{
		"action": "agenda", "id": p.ID, "revision": 2, "agenda_choices": []map[string]any{{"kind": "question", "id": "stale", "included": true}},
	}, intern, 422)
	var status string
	if err := db.QueryRow(`SELECT status FROM knowledge_gaps WHERE id=$1`, question).Scan(&status); err != nil || status == "resolved" {
		t.Fatal(status, err)
	}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", path, strings.NewReader(mutate("accept", 2)))
			r.Header.Set("Origin", "http://localhost:5173")
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(mentor)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)
	for code := range results {
		if code != 200 {
			t.Fatal(code)
		}
	}
	apiRequest(t, h, "POST", path, mutate("decline", 2), mentor, 409)
	if body := apiRequest(t, h, "GET", path, "", intern, 200).Body.String(); !strings.Contains(body, `"status":"accepted"`) {
		t.Fatal(body)
	}
	w := apiRequest(t, h, "GET", calendar, "", intern, 200)
	for _, match := range []string{"DTSTART:" + strings.ReplaceAll(date, "-", "") + "T120000Z", "Europe/Moscow", "https://example.com/call", "#meeting-" + p.ID} {
		if !strings.Contains(strings.ReplaceAll(w.Body.String(), "\r\n ", ""), match) {
			t.Fatal(match, w.Body.String())
		}
	}
	apiRequest(t, h, "GET", calendar, "", foreign, 404)
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{"action": "reschedule", "id": p.ID, "revision": 3, "scheduled_at": at.Add(time.Hour), "timezone": "UTC"}, intern, 200)
	apiRequest(t, h, "GET", calendar, "", intern, 404)
	apiRequest(t, h, "POST", path, mutate("accept", 2), mentor, 409)
	apiRequest(t, h, "POST", path, mutate("decline", 4), mentor, 200)
	apiRequest(t, h, "POST", path, mutate("decline", 4), mentor, 200)
	if body := apiRequest(t, h, "GET", path, "", intern, 200).Body.String(); !strings.Contains(body, `"status":"declined"`) {
		t.Fatal(body)
	}
	legacy := "c0000000-0000-4000-8000-000000000001"
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{"id": legacy, "action": "create", "intern_id": "intern_demo_1", "scheduled_at": at, "timezone": "UTC"}, mentor, 200)
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{"id": legacy, "action": "complete", "revision": 1, "content": "Результат",
		"agreements": []map[string]any{{"content": "Согласовать поставку", "owner_id": "mentor_demo_1", "step_id": "step_local_setup", "due_at": at.AddDate(0, 0, -10)}}}, mentor, 200)
	if proposed := suggest("Просрочена договорённость и не могу продолжить"); proposed == nil || proposed.Reason != "overdue" {
		t.Fatal(proposed)
	}
	second := "c0000000-0000-4000-8000-000000000002"
	for _, invalid := range []map[string]any{
		{"id": second, "action": "invite", "mentor_id": p.Mentor, "title": p.Title, "local_at": "2027-11-07T01:30", "timezone": "America/New_York"},
		{"id": second, "action": "invite", "mentor_id": p.Mentor, "title": p.Title, "local_at": "2030-01-01T12:30", "timezone": "Moon"},
		{"id": second, "action": "invite", "mentor_id": p.Mentor, "title": p.Title, "local_at": "2030-01-01T12:30", "timezone": "UTC", "scheduled_at": at},
		{"id": second, "action": "invite", "mentor_id": p.Mentor, "title": p.Title, "scheduled_at": at.AddDate(0, 0, -10), "timezone": "UTC"},
	} {
		hrResponse[any](t, h, "POST", "/meetings", invalid, intern, 422)
	}
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{"id": second, "action": "create", "scheduled_at": at, "timezone": "UTC"}, intern, 200)
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{"id": second, "action": "details", "revision": 1, "title": "Новая тема", "call_url": "https://example.org/"}, intern, 200)
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{"id": second, "action": "topics-add", "revision": 2, "topics": []string{"Новый пункт"}}, intern, 200)
	var topic string
	if err := db.QueryRow(`SELECT id FROM onboarding_meeting_topics WHERE meeting_id=$1`, second).Scan(&topic); err != nil {
		t.Fatal(err)
	}
	deleteTopic := map[string]any{"id": second, "action": "topic-delete", "revision": 3, "topic_id": topic}
	hrResponse[any](t, h, "POST", "/meetings", deleteTopic, mentor, 404)
	hrResponse[any](t, h, "POST", "/meetings", deleteTopic, intern, 200)
	hrResponse[any](t, h, "POST", "/meetings", map[string]any{"id": second, "action": "cancel", "revision": 4}, intern, 200)
	apiRequest(t, h, "GET", path+"/"+second+"/calendar", "", intern, 404)
	apiRequest(t, h, "GET", path+"/invalid/calendar", "", intern, 404)

	if _, err := db.Exec(`UPDATE mentor_assignments SET mentor_id='mentor_demo_2' WHERE intern_id='intern_demo_1'`); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "POST", path, mutate("decline", 4), mentor, 404)
	apiRequest(t, h, "POST", path, mutate("decline", 4), foreign, 404)
}
