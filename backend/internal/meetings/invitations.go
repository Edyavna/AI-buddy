package meetings

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type agendaChoice struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Included bool   `json:"included"`
}

func validChoices(choices []agendaChoice) bool {
	if len(choices) > 100 {
		return false
	}
	seen := make(map[[2]string]bool)
	for _, c := range choices {
		key := [2]string{c.Kind, c.ID}
		if seen[key] || !validContent(c.ID, 200) {
			return false
		}
		switch c.Kind {
		case "checkpoint", "question", "agreement":
		default:
			return false
		}
		seen[key] = true
	}
	return true
}

func validCallURL(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	return len(value) <= 2048 && err == nil && parsed.Hostname() != "" && utf8.ValidString(parsed.Hostname()) && parsed.User == nil &&
		(parsed.Scheme == "https" || parsed.Scheme == "http")
}

func editableStatus(status string) bool {
	return status == "scheduled" || status == "pending" || status == "accepted"
}

func decide(ctx context.Context, tx *sql.Tx, in change, actor, status string, revision int64, previous sql.NullInt64) (int64, error) {
	next := "accepted"
	if in.Action == "decline" {
		next = "declined"
	}
	if status == next && previous.Valid && previous.Int64 == in.Revision {
		return revision, nil
	}
	if status != "pending" || in.Revision != revision {
		return 0, ports.ErrConflict
	}
	_, err := tx.ExecContext(ctx, `UPDATE onboarding_meetings SET status=$2,last_decision_revision=$3 WHERE id=$1`, in.ID, next, revision)
	if err != nil {
		return 0, err
	}
	return revision + 1, record(ctx, tx, in.ID, actor, in.Action, revision+1)
}

func saveChoices(ctx context.Context, tx *sql.Tx, in change) error {
	for _, c := range in.Choices {
		result, err := tx.ExecContext(ctx, `INSERT INTO onboarding_meeting_agenda_choices(meeting_id,kind,item_id,included)
   SELECT m.id,$2,$3,$4 FROM onboarding_meetings m
   LEFT JOIN LATERAL (`+previousMeeting+`) prev ON true
   WHERE m.id=$1 AND EXISTS (SELECT 1 FROM (`+agendaRows+`) item WHERE item.kind=$2 AND item.key=$3)
   ON CONFLICT(meeting_id,kind,item_id) DO UPDATE SET included=EXCLUDED.included`, in.ID, c.Kind, c.ID, c.Included)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			if err != nil {
				return err
			}
			return ports.ErrInvalid
		}
	}
	return nil
}

func deleteTopic(ctx context.Context, tx *sql.Tx, in change, actor string) error {
	result, err := tx.ExecContext(ctx, `DELETE FROM onboarding_meeting_topics WHERE id=$1 AND meeting_id=$2 AND author_id=$3`, in.Topic, in.ID, actor)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return err
}

func (s Store) calendar(w http.ResponseWriter, r *http.Request) {
	p, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	id := r.PathValue("id")
	if !uuid(id) {
		writeError(w, r, ports.ErrNotFound)
		return
	}
	var at time.Time
	var revision int64
	var zone, title, call string
	err = s.DB.QueryRowContext(r.Context(), `SELECT m.scheduled_at,m.timezone,m.title,m.call_url,m.revision`+visible+
		` WHERE m.id=$4 AND m.status='accepted'`, p.UserID, p.TeamID, p.Role, id).Scan(&at, &zone, &title, &call, &revision)
	if err != nil {
		writeError(w, r, err)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	page := "/meetings"
	if p.Role == identity.Mentor {
		page = "/mentor/meetings"
	}
	host := r.Host
	if ref, err := url.Parse(r.Referer()); err == nil && slices.Contains(s.Origins, ref.Scheme+"://"+ref.Host) {
		scheme, host = ref.Scheme, ref.Host
	}
	link := (&url.URL{Scheme: scheme, Host: host, Path: page, Fragment: "meeting-" + id}).String()
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//AI-buddy//Meetings//RU", "CALSCALE:GREGORIAN",
		"X-WR-TIMEZONE:" + calendarText(zone), "BEGIN:VEVENT", "UID:" + id + "@ai-buddy",
		fmt.Sprintf("SEQUENCE:%d", revision), "DTSTAMP:" + calendarTime(time.Now()), "DTSTART:" + calendarTime(at),
		"SUMMARY:" + calendarText(title), "URL:" + link}
	description := "Часовой пояс: " + zone
	if call != "" {
		description += "\n" + call
	}
	lines = append(lines, "DESCRIPTION:"+calendarText(description), "END:VEVENT", "END:VCALENDAR")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="meeting.ics"`)
	for _, line := range lines {
		fmt.Fprint(w, foldCalendarLine(line), "\r\n")
	}
}

func calendarTime(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

func calendarText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, text)
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, ";", `\;`, ",", `\,`).Replace(text)
}

func foldCalendarLine(line string) string {
	var out strings.Builder
	limit := 75
	for len(line) > limit {
		end := limit
		for !utf8.RuneStart(line[end]) {
			end--
		}
		out.WriteString(line[:end])
		out.WriteString("\r\n ")
		line = line[end:]
		limit = 74
	}
	out.WriteString(line)
	return out.String()
}

func validTopics(topics []string) bool {
	for _, topic := range topics {
		if !validContent(topic, 2000) {
			return false
		}
	}
	return true
}

func addTopics(ctx context.Context, tx *sql.Tx, in change, actor string) error {
	for _, topic := range in.Topics {
		if _, err := tx.ExecContext(ctx, `INSERT INTO onboarding_meeting_topics(id,meeting_id,author_id,content)
  VALUES(gen_random_uuid(),$1,$2,$3)`, in.ID, actor, topic); err != nil {
			return err
		}
	}
	return nil
}
