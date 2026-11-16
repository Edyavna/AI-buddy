package personalmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type StatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}
type RequestSummary struct {
	ID         string `json:"id"`
	Resource   string `json:"resource_name"`
	Permission string `json:"permission"`
	Status     string `json:"status"`
}
type LiveContext struct {
	Team     Person           `json:"team"`
	Role     string           `json:"role"`
	Mentor   *Person          `json:"mentor"`
	Progress *int             `json:"progress_percent"`
	Requests []StatusCount    `json:"requests"`
	Recent   []RequestSummary `json:"recent_requests"`
}
type Workspace struct {
	Entries  []Entry     `json:"entries"`
	Pending  []Proposal  `json:"pending"`
	Options  []Option    `json:"options"`
	Context  LiveContext `json:"context"`
	Markdown string      `json:"profile_markdown"`
}

func liveContext(ctx context.Context, tx *sql.Tx, p identity.Principal) (LiveContext, error) {
	out := LiveContext{Role: string(p.Role), Requests: []StatusCount{}, Recent: []RequestSummary{}}
	err := tx.QueryRowContext(ctx, `SELECT t.id,t.name FROM users u JOIN teams t ON t.id=u.team_id WHERE u.id=$1 AND u.team_id=$2 AND u.role='intern'`, p.UserID, p.TeamID).Scan(&out.Team.ID, &out.Team.Name)
	if err != nil {
		return out, err
	}
	var mentor Person
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.name FROM mentor_assignments a JOIN users u ON u.id=a.mentor_id AND u.team_id=a.team_id AND u.role='mentor' WHERE a.intern_id=$1 AND a.team_id=$2 FOR SHARE OF a,u`, p.UserID, p.TeamID).Scan(&mentor.ID, &mentor.Name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		out.Mentor = &mentor
	}
	plan, err := onboarding.ReadOnlyPlanTx(ctx, tx, p)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return out, err
	}
	if err == nil {
		out.Progress = &plan.ProgressPercent
	}
	rows, err := tx.QueryContext(ctx, `SELECT status,count(*) FROM access_requests WHERE user_id=$1 AND team_id=$2 GROUP BY status ORDER BY status`, p.UserID, p.TeamID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var r StatusCount
		if err = rows.Scan(&r.Status, &r.Count); err != nil {
			rows.Close()
			return out, err
		}
		out.Requests = append(out.Requests, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,resource_name,permission,status FROM access_requests WHERE user_id=$1 AND team_id=$2 ORDER BY updated_at DESC,id LIMIT 5`, p.UserID, p.TeamID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r RequestSummary
		if err = rows.Scan(&r.ID, &r.Resource, &r.Permission, &r.Status); err != nil {
			return out, err
		}
		out.Recent = append(out.Recent, r)
	}
	return out, rows.Err()
}
func dataName(name string) string {
	r := []rune(name)
	if len(r) > 120 {
		r = r[:120]
	}
	data, _ := json.Marshal(string(r))
	return string(data)
}
func markdown(live LiveContext, items []Entry) string {
	var out strings.Builder
	out.WriteString("# Персональный профиль стажёра\n\n## Текущие данные приложения\n")
	fmt.Fprintf(&out, "- Команда: %s\n- Роль: %s\n", dataName(live.Team.Name), dataName(live.Role))
	if live.Mentor == nil {
		out.WriteString("- Наставник: не назначен\n")
	} else {
		fmt.Fprintf(&out, "- Действующий наставник: %s\n", dataName(live.Mentor.Name))
	}
	if live.Progress == nil {
		out.WriteString("- План ещё не создан\n")
	} else {
		fmt.Fprintf(&out, "- Прогресс плана: %d%%\n", *live.Progress)
	}
	for _, r := range live.Requests {
		fmt.Fprintf(&out, "- Заявки %s: %d\n", dataName(r.Status), r.Count)
	}
	out.WriteString("\n## Подтверждённые пользователем рабочие предпочтения\n")
	if len(items) == 0 {
		out.WriteString("Нет сохранённых предпочтений.\n")
	}
	for _, item := range items {
		line := "- " + item.Text + "\n"
		if out.Len()+len(line) > 4096 {
			break
		}
		out.WriteString(line)
	}
	if len(live.Recent) > 0 {
		heading := "\n## Последние заявки (до пяти, без причин и персональных подробностей)\n"
		if out.Len()+len(heading) < 4096 {
			out.WriteString(heading)
		}
		for _, r := range live.Recent {
			line := fmt.Sprintf("- %s: %s, %s, %s\n", dataName(r.ID), dataName(r.Resource), dataName(r.Permission), dataName(r.Status))
			if out.Len()+len(line) > 4096 {
				break
			}
			out.WriteString(line)
		}
	}
	return out.String()
}
func (s Store) Workspace(ctx context.Context) (Workspace, error) {
	out := Workspace{Entries: []Entry{}, Pending: []Proposal{}, Options: Options()}
	err := s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		var err error
		out.Entries, err = entries(ctx, tx, p)
		if err != nil {
			return err
		}
		out.Pending, err = proposals(ctx, tx, p, "")
		if err != nil {
			return err
		}
		out.Context, err = liveContext(ctx, tx, p)
		if err != nil {
			return err
		}
		out.Markdown = markdown(out.Context, out.Entries)
		return nil
	})
	return out, err
}
func (s Store) Profile(ctx context.Context) (string, error) {
	var result string
	err := s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		items, err := entries(ctx, tx, p)
		if err != nil {
			return err
		}
		live, err := liveContext(ctx, tx, p)
		if err != nil {
			return err
		}
		result = markdown(live, items)
		return nil
	})
	return result, err
}

type ProfileReader interface {
	Profile(context.Context) (string, error)
}

// Each request loads a new snapshot. No profile is cached or taken from the client.
// A failed optional profile read uses no memory, never a stale copy.
type Agent struct {
	Reader ProfileReader
	Next   ports.Agent
}

func (a Agent) ConversationTitle(ctx context.Context, text string) string {
	if generator, ok := a.Next.(interface {
		ConversationTitle(context.Context, string) string
	}); ok {
		return generator.ConversationTitle(ctx, text)
	}
	return ""
}

func (a Agent) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	profile, err := a.Reader.Profile(ctx)
	if err != nil && !errors.Is(err, ports.ErrUnavailable) {
		return ports.AgentResult{}, err
	}
	if err != nil {
		profile = ""
	}
	if safeSource(in.Text) && rememberPrevious.MatchString(in.Text) {
		err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: "Если в предыдущей реплике есть подходящее предпочтение, оно появится в карточке. Нажмите «Запомнить», чтобы добавить его в память."})
		return ports.AgentResult{Outcome: ports.OutcomeAnswered}, err
	}
	in.PersonalProfile = profile
	return a.Next.Run(ctx, in, emit)
}
