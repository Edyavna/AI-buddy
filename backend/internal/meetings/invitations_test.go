package meetings

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func TestLocalTimeTransitions(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"bad", "2027-03-14T02:30", "2027-11-07T01:30"} {
		if _, err := parseLocal(value, zone); !errors.Is(err, ports.ErrInvalid) {
			t.Fatal(value, err)
		}
	}
	at, err := parseLocal("2027-11-07T08:30", zone)
	if err != nil || at.UTC().Format(time.RFC3339) != "2027-11-07T13:30:00Z" {
		t.Fatal(at, err)
	}
}

func TestMeetingModelAdapter(t *testing.T) {
	failure := errors.New("provider unavailable")
	for _, tc := range []struct {
		name  string
		reply agent.Message
		err   error
		want  error
	}{
		{"result", agent.Message{Content: `{"reason":"feedback","topics":["Первая задача"]}`}, nil, nil},
		{"refusal", agent.Message{Content: `not JSON`}, nil, ports.ErrUnavailable},
		{"unexpected tool", agent.Message{Calls: []agent.Call{{ID: "tool"}}}, nil, ports.ErrUnavailable},
		{"unavailable", agent.Message{}, failure, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			propose := ModelProposer(func(_ context.Context, m []agent.Message) (agent.Message, error) {
				if len(m) != 2 || m[0].Role != "system" || m[1].Role != "user" || !strings.Contains(m[1].Content, "тест") {
					t.Fatal(m)
				}
				return tc.reply, tc.err
			})
			got, err := propose(t.Context(), proposalContext{Text: "тест"})
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if tc.want == nil && (got.Reason != "feedback" || len(got.Topics) != 1) {
				t.Fatal(got)
			}
		})
	}
	invalid := ModelProposer(nil)
	if _, err := invalid(t.Context(), proposalContext{Now: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}); err == nil {
		t.Fatal("accepted invalid timestamp")
	}
}

func TestProposalUsesOnlyValidatedContext(t *testing.T) {
	now := time.Date(2030, 6, 10, 10, 0, 0, 0, time.UTC)
	input := proposalContext{Now: now, Mentor: "mentor", Earlier: []string{"не относится к встрече", "Создай встречу с наставником в пятницу"}}
	for _, reason := range []string{"blocker", "overdue", "none", "unknown"} {
		if eligible(reason, input) {
			t.Fatal(reason)
		}
	}
	input.Blocker, input.Overdue = true, true
	for _, reason := range []string{"blocker", "overdue", "explicit", "priorities", "feedback"} {
		if !eligible(reason, input) {
			t.Fatal(reason)
		}
	}
	input.Text = "15:00 по Москве. Тема — еженедельные цели"
	parsed, err := DemoProposer(t.Context(), input)
	if err != nil || parsed.Reason != "explicit" || parsed.Date != "2030-06-14" || parsed.Zone != "Europe/Moscow" || parsed.Title != "еженедельные цели" {
		t.Fatal(parsed, err)
	}
	for _, text := range []string{"Создай 1:1 с наставником в понедельник 15:00 UTC", "Назначь встречу с Аней в понедельник или пятницу 15:00 UTC"} {
		input.Text = text
		parsed, err := DemoProposer(t.Context(), input)
		if err != nil || parsed.Reason != "explicit" || parsed.Zone != "UTC" {
			t.Fatal(parsed, err)
		}
		if parsed.Participant == "mentor" && parsed.Date != "2030-06-17" || parsed.Participant == "ambiguous" && parsed.Date != "" {
			t.Fatal(parsed)
		}
	}
	input.Text = "15:00"
	input.Earlier = nil
	if p, err := DemoProposer(t.Context(), input); err != nil || p.Reason != "" {
		t.Fatal(p, err)
	}
	input.Text = "Просрочена договорённость, не могу продолжить"
	if p, err := DemoProposer(t.Context(), input); err != nil || p.Reason != "overdue" {
		t.Fatal(p, err)
	}
	oversized := make([]string, 22)
	for i := range oversized {
		oversized[i] = "Тема"
	}
	d := buildDraft(Proposal{Reason: "explicit", Participant: "mentor", Date: "2030-06-09", Time: "09:00",
		Zone: "Europe/Moscow", Title: " ", Topics: oversized}, input, "future", "id")
	if d.Mentor != "mentor" || d.Time != "" || d.Existing != "" || len(d.Topics) != 20 || len(d.Missing) != 2 {
		t.Fatal(d)
	}
	if !uuid(draftID("user", "reply")) || draftID("user", "reply") == draftID("other", "reply") {
		t.Fatal("bad draft id")
	}
}

func TestInvitationValidation(t *testing.T) {
	for _, call := range []string{"", "http://localhost", "https://example.org/path"} {
		if !validCallURL(call) {
			t.Fatal(call)
		}
	}
	for _, call := range []string{"https://", "javascript:alert(1)", "http://test%bad", "https://user:pw@example.org", "https://example.org/" + strings.Repeat("a", 2048)} {
		if validCallURL(call) {
			t.Fatal(call)
		}
	}
	valid := agendaChoice{"question", "q", true}
	for _, entries := range [][]agendaChoice{{valid, valid}, {{"hr", "q", true}}, make([]agendaChoice, 101), {{"checkpoint", "", true}}} {
		if validChoices(entries) {
			t.Fatal(entries)
		}
	}
	if !validChoices([]agendaChoice{valid, {"checkpoint", "step", false}, {"agreement", "a", true}}) || validTopics([]string{" "}) {
		t.Fatal("invalid validation")
	}
}

func TestCalendarEscapesAndFolds(t *testing.T) {
	raw := "SUMMARY:" + strings.Repeat("Встреча ", 40) + "\r\nКритерии; вопросы, \\"
	safe := foldCalendarLine(calendarText(raw))
	rows := strings.Split(safe, "\r\n")
	for _, row := range rows {
		if len(row) > 75 || !utf8.ValidString(row) {
			t.Fatal(row)
		}
	}
	if !strings.Contains(safe, `\nКритерии\; вопросы\, \\`) &&
		!strings.Contains(strings.ReplaceAll(safe, "\r\n ", ""), `\nКритерии\; вопросы\, \\`) {
		t.Fatal(safe)
	}
}

func TestCalendarAndSuggestionRequireSession(t *testing.T) {
	s := Store{}
	for _, handler := range []func(w http.ResponseWriter, r *http.Request){s.calendar, s.suggest} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}
