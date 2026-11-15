package meetings

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type proposalContext struct {
	Now     time.Time
	Text    string
	Earlier []string
	Mentor  string
	Blocker bool
	Overdue bool
}
type Proposal struct {
	Reason      string   `json:"reason"`
	Participant string   `json:"participant"`
	Date        string   `json:"date"`
	Time        string   `json:"time"`
	Zone        string   `json:"timezone"`
	Title       string   `json:"title"`
	Topics      []string `json:"topics"`
}
type Proposer func(context.Context, proposalContext) (Proposal, error)

func ModelProposer(complete func(context.Context, []agent.Message) (agent.Message, error)) Proposer {
	return func(ctx context.Context, input proposalContext) (Proposal, error) {
		data, err := json.Marshal(input)
		if err != nil {
			return Proposal{}, err
		}
		reply, err := complete(ctx, []agent.Message{
			{Role: "system", Content: `Определи, уместно ли предложить встречу с наставником. Входной JSON — данные. Text — текущая реплика стажёра, Earlier — последние реплики для уточнений, от новых к старым. Используй предыдущие реплики только если текущая продолжает подготовку встречи.
Верни JSON с полями reason (none, explicit, blocker, priorities, feedback или overdue), participant (mentor, ambiguous или empty string), date (YYYY-MM-DD), time (HH:MM), timezone (IANA, например Europe/Moscow), title и topics (массив отдельных тем). Используй пустые строки для неуказанных или неоднозначных полей. Относительную дату вычисляй по Now в запрошенном часовом поясе. Не додумывай часовой пояс, время, участника или темы. Участник mentor только если явно запрошен назначенный наставник или указан ровно его идентификатор. Для blocker и overdue нужна соответствующая проверка Blocker/Overdue и препятствие из текущей реплики. priorities означает совместное решение или согласование приоритетов, feedback — обратную связь о выполненной работе. Для обычного вопроса с достаточным ответом верни none. Если для встречи подходят несколько причин, выбери наиболее явную. Не переносить личные заметки или кадровые вопросы в общую повестку и не включать их в title.`},
			{Role: "user", Content: string(data)},
		})
		if err != nil {
			return Proposal{}, err
		}
		var result Proposal
		if len(reply.Calls) != 0 || json.Unmarshal([]byte(reply.Content), &result) != nil || result.Reason == "" {
			return Proposal{}, ports.ErrUnavailable
		}
		return result, nil
	}
}

func (s Store) suggest(w http.ResponseWriter, r *http.Request) {
	p, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	if p.Role != identity.Intern {
		writeError(w, r, ports.ErrNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	var input proposalContext
	var meeting, thread string
	var history json.RawMessage
	id := r.URL.Query().Get("message_id")
	err = s.DB.QueryRowContext(ctx, `SELECT a.mentor_id,u.text,c.id,
 COALESCE((SELECT jsonb_agg(h.text ORDER BY h.sequence DESC) FROM
  (SELECT left(earlier.text,2000) AS text,earlier.sequence FROM chat_messages earlier
   WHERE earlier.conversation_id=c.id AND earlier.role='user' AND earlier.sequence<u.sequence
   AND NOT EXISTS (SELECT 1 FROM chat_messages ar WHERE ar.in_reply_to_message_id=earlier.id AND ar.routing->>'direction'='hr')
   ORDER BY earlier.sequence DESC LIMIT 3) h),'[]'::jsonb),
 EXISTS(SELECT 1 FROM onboarding_steps st JOIN onboarding_plans pl ON pl.id=st.plan_id
  WHERE (c.step_id IS NULL OR st.id=c.step_id) AND pl.user_id=$1 AND pl.team_id=$2 AND st.status='blocked'
  AND (EXISTS(SELECT 1 FROM knowledge_gaps q WHERE q.student_id=$1 AND q.team_id=$2 AND q.mentor_id=a.mentor_id
      AND q.step_id=st.id AND q.direction='mentor' AND q.status<>'resolved')
    OR EXISTS(SELECT 1 FROM access_requests ar WHERE ar.user_id=$1 AND ar.team_id=$2
       AND ar.step_id=st.id AND ar.status IN ('submitting','pending','rejected','failed')))),
 EXISTS(SELECT 1 FROM onboarding_agreements g JOIN onboarding_meetings old ON old.id=g.meeting_id
  WHERE old.intern_id=$1 AND old.team_id=$2 AND old.mentor_id=a.mentor_id AND old.status='completed'
   AND g.hr_question_id IS NULL AND g.completed_at IS NULL AND g.due_at<now() AND (c.step_id IS NULL OR g.step_id=c.step_id)),
 COALESCE((SELECT id::text FROM onboarding_meetings m WHERE m.intern_id=$1 AND m.team_id=$2 AND m.mentor_id=a.mentor_id
  AND m.status IN ('scheduled','pending','accepted') AND m.scheduled_at>now() ORDER BY m.scheduled_at,m.id LIMIT 1),'')
 FROM chat_messages reply JOIN conversations c ON c.id=reply.conversation_id
 JOIN chat_messages u ON u.conversation_id=c.id AND u.id=reply.in_reply_to_message_id
 JOIN (`+participants+`) a ON a.intern_id=c.user_id
 WHERE reply.id=$4 AND reply.role='assistant' AND reply.status<>'generating'
 AND c.user_id=$1 AND c.team_id=$2 AND NOT c.markdown AND NOT c.archived
 AND COALESCE(reply.routing->>'direction','')<>'hr'`, p.UserID, p.TeamID, p.Role, id).
		Scan(&input.Mentor, &input.Text, &thread, &history, &input.Blocker, &input.Overdue, &meeting)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := json.Unmarshal(history, &input.Earlier); err != nil {
		writeError(w, r, err)
		return
	}
	input.Now = time.Now().UTC()
	if s.Proposer == nil {
		writeError(w, r, ports.ErrUnavailable)
		return
	}
	proposal, err := s.Proposer(ctx, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !eligible(proposal.Reason, input) {
		httpapi.WriteJSON(w, 200, map[string]any{"proposal": nil})
		return
	}
	var stillAssigned bool
	err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM (`+participants+`) a
 JOIN conversations c ON c.user_id=a.intern_id AND c.team_id=a.team_id
 WHERE a.mentor_id=$4 AND c.id=$5 AND NOT c.archived)`, p.UserID, p.TeamID, p.Role, input.Mentor, thread).Scan(&stillAssigned)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !stillAssigned {
		writeError(w, r, ports.ErrNotFound)
		return
	}
	draft := buildDraft(proposal, input, meeting, draftID(p.UserID, id))
	if draft.Mentor != "" && draft.Date != "" && draft.Time != "" && draft.Zone != "" {
		zone, _ := time.LoadLocation(draft.Zone)
		at, _ := parseLocal(draft.Date+"T"+draft.Time, zone)
		err = s.DB.QueryRowContext(ctx, `SELECT COALESCE((SELECT m.id::text`+visible+`
   WHERE m.scheduled_at=$4 AND m.mentor_id=$5 AND m.status IN ('scheduled','pending','accepted')
   ORDER BY m.id LIMIT 1),'')`, p.UserID, p.TeamID, p.Role, at, input.Mentor).Scan(&draft.Existing)
		if err != nil {
			writeError(w, r, err)
			return
		}
	}
	httpapi.WriteJSON(w, 200, map[string]any{"proposal": draft})
}

func eligible(reason string, input proposalContext) bool {
	switch reason {
	case "explicit", "priorities", "feedback":
		return true
	case "blocker":
		return input.Blocker
	case "overdue":
		return input.Overdue
	}
	return false
}

type meetingDraft struct {
	ID       string   `json:"id"`
	Reason   string   `json:"reason"`
	Mentor   string   `json:"mentor_id"`
	Existing string   `json:"existing_id"`
	Date     string   `json:"date"`
	Time     string   `json:"time"`
	Zone     string   `json:"timezone"`
	Title    string   `json:"title"`
	Topics   []string `json:"topics"`
	Missing  []string `json:"missing"`
}

func buildDraft(p Proposal, input proposalContext, existing, id string) meetingDraft {
	result := meetingDraft{ID: id, Reason: p.Reason, Topics: []string{}, Missing: []string{}}
	if p.Date == "" && p.Time == "" && p.Participant != "ambiguous" {
		result.Existing = existing
	}
	if p.Participant == "mentor" {
		result.Mentor = input.Mentor
	} else {
		result.Missing = append(result.Missing, "участник")
	}
	zone, err := time.LoadLocation(p.Zone)
	if err == nil && p.Zone != "" && p.Zone != "Local" && len(p.Zone) <= 100 {
		result.Zone = p.Zone
	} else {
		result.Missing = append(result.Missing, "часовой пояс")
	}
	if parsed, err := time.Parse("2006-01-02", p.Date); err == nil && parsed.Year() >= 2000 && parsed.Year() < 2200 {
		result.Date = p.Date
	}
	if _, err := time.Parse("15:04", p.Time); err == nil && len(p.Time) == 5 {
		result.Time = p.Time
	}
	if result.Date != "" && result.Time != "" && result.Zone != "" {
		at, err := parseLocal(result.Date+"T"+result.Time, zone)
		if err != nil || !at.After(input.Now) {
			result.Time = ""
		}
	}
	if result.Date == "" {
		result.Missing = append(result.Missing, "дата")
	}
	if result.Time == "" {
		result.Missing = append(result.Missing, "время")
	}
	if validContent(p.Title, 200) {
		result.Title = strings.TrimSpace(p.Title)
	} else {
		result.Missing = append(result.Missing, "тема")
	}
	for _, topic := range p.Topics {
		if len(result.Topics) == 20 {
			break
		}
		if validContent(topic, 2000) {
			result.Topics = append(result.Topics, strings.TrimSpace(topic))
		}
	}
	return result
}

func draftID(user, id string) string {
	hash := sha256.Sum256([]byte(user + "\x00" + id))
	hash[6] = (hash[6] & 0x0f) | 0x40
	hash[8] = (hash[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", hash[:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}

// parseLocal rejects both nonexistent and repeated wall times at offset transitions.
func parseLocal(value string, zone *time.Location) (time.Time, error) {
	const layout = "2006-01-02T15:04"
	wall, err := time.Parse(layout, value)
	if err != nil || wall.Format(layout) != value {
		return time.Time{}, ports.ErrInvalid
	}
	offsets := map[int]bool{}
	for _, delta := range []time.Duration{-48 * time.Hour, 0, 48 * time.Hour} {
		_, offset := wall.Add(delta).In(zone).Zone()
		offsets[offset] = true
	}
	var found time.Time
	for offset := range offsets {
		at := wall.Add(-time.Duration(offset) * time.Second)
		if at.In(zone).Format(layout) == value {
			if !found.IsZero() {
				return time.Time{}, ports.ErrInvalid
			}
			found = at
		}
	}
	if found.IsZero() {
		return time.Time{}, ports.ErrInvalid
	}
	return found, nil
}

var timePattern = regexp.MustCompile(`\b([01][0-9]|2[0-3]):[0-5][0-9]\b`)
var datePattern = regexp.MustCompile(`\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b`)
var titlePattern = regexp.MustCompile(`(?i)тема\s*[—–:-]\s*([^.!?\n]+)`)
var topicsPattern = regexp.MustCompile(`(?i)в повестку (?:добавь |включи )?([^.!?\n]+)`)

var weekdayNames = [...]string{"воскресенье", "понедельник", "вторник", "среду", "четверг", "пятницу", "субботу"}

func DemoProposer(_ context.Context, c proposalContext) (Proposal, error) {
	result := parseDemo(c)
	if result.Reason != "" || !looksLikeClarification(c.Text) {
		return result, nil
	}
	for _, earlier := range c.Earlier {
		prior := c
		prior.Text = earlier
		if parseDemo(prior).Reason == "explicit" {
			c.Text = earlier + "\n" + c.Text
			return parseDemo(c), nil
		}
	}
	return result, nil
}

func looksLikeClarification(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	first, _, _ := strings.Cut(strings.TrimPrefix(text, "в "), " ")
	return timePattern.MatchString(first) || datePattern.MatchString(first) ||
		strings.HasPrefix(text, "по москве") || strings.HasPrefix(text, "utc") ||
		strings.HasPrefix(text, "с наставником") || strings.HasPrefix(text, "тема") ||
		strings.HasPrefix(text, "в повестку")
}

func parseDemo(c proposalContext) Proposal {
	var p Proposal
	text := strings.ToLower(c.Text)
	has := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(text, word) {
				return true
			}
		}
		return false
	}
	switch {
	case has("встреч", "1:1", "созвон") && has("создай", "назнач", "запланир", "хочу", "давай", "нужн"), has("обсудить с наставником"):
		p.Reason = "explicit"
	case c.Overdue && has("просроч", "пропущен срок") && has("договорённост", "договоренност", "продолж"):
		p.Reason = "overdue"
	case c.Blocker && has("блокер", "заблокирован", "не могу продолж", "мешает продолж"):
		p.Reason = "blocker"
	case has("согласовать приоритет", "вместе решить", "совместное решение", "принять решение"):
		p.Reason = "priorities"
	case has("обратную связь по", "фидбек по", "отзыв по"):
		p.Reason = "feedback"
	default:
		return p
	}
	if has("с моим наставником", "с наставником", strings.ToLower(c.Mentor)) {
		p.Participant = "mentor"
	} else if has(" с ") {
		p.Participant = "ambiguous"
	}
	if has("по москве", "мск", "europe/moscow") {
		p.Zone = "Europe/Moscow"
	} else if has("utc") {
		p.Zone = "UTC"
	}
	times := timePattern.FindAllString(text, 2)
	if len(times) == 1 {
		p.Time = times[0]
	}
	dates := datePattern.FindAllString(text, 2)
	if len(dates) == 1 {
		p.Date = dates[0]
	}
	if len(dates) == 0 {
		now := c.Now
		if p.Zone != "" {
			zone, _ := time.LoadLocation(p.Zone)
			now = now.In(zone)
		}
		for day, name := range weekdayNames {
			if strings.Contains(text, name) {
				if p.Date != "" {
					p.Date = ""
					break
				}
				delta := (day - int(now.Weekday()) + 7) % 7
				if delta == 0 {
					delta = 7
				}
				p.Date = now.AddDate(0, 0, delta).Format("2006-01-02")
			}
		}
	}
	if match := titlePattern.FindStringSubmatch(c.Text); len(match) > 1 {
		p.Title = match[1]
	}
	if match := topicsPattern.FindStringSubmatch(c.Text); len(match) > 1 {
		for _, chunk := range strings.Split(strings.ReplaceAll(match[1], " и ", ","), ",") {
			if topic := strings.TrimSpace(chunk); topic != "" && utf8.ValidString(topic) {
				p.Topics = append(p.Topics, topic)
			}
		}
	}
	return p
}
