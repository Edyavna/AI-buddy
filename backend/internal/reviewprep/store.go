package reviewprep

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// Store сохраняет подготовки и шаблоны; права проверяются по Principal в каждом методе.
type Store struct {
	DB       *sql.DB
	Provider PRProvider // nil — ручной режим без интеграции GitHub
}

func mapped(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ports.ErrNotFound
	case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrInvalid), errors.Is(err, ports.ErrForbidden),
		errors.Is(err, ports.ErrMentorUnassigned), errors.Is(err, ErrChanged), errors.Is(err, ErrTransition),
		errors.Is(err, ErrPRNotFound), errors.Is(err, ErrPRRateLimit):
		return err
	default:
		return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
	}
}

func principal(ctx context.Context, role identity.Role) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err == nil && p.Role != role {
		err = ports.ErrForbidden
	}
	return p, err
}

func validStatus(s string) bool { return s == "passed" || s == "failed" || s == "skipped" }

// clamp обрезает пользовательский текст до предела поля.
func clamp(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return string(runes)
}

func normalizeAnswers(in Answers) Answers {
	out := Answers{
		Summary:      clamp(in.Summary, 20000),
		Verification: clamp(in.Verification, 20000),
		Limitations:  clamp(in.Limitations, 20000),
		Questions:    clamp(in.Questions, 20000),
		Items:        map[string]ItemAnswer{},
	}
	for key, item := range in.Items {
		key = clamp(key, 100)
		if key == "" {
			continue
		}
		out.Items[key] = ItemAnswer{Done: item.Done, Note: clamp(item.Note, 2000)}
	}
	return out
}

// validateAnswers отклоняет переполненные поля, а не молча их обрезает.
func validateAnswers(in Answers) error {
	for _, value := range []string{in.Summary, in.Verification, in.Limitations, in.Questions} {
		if len([]rune(value)) > 20000 {
			return ports.ErrInvalid
		}
	}
	for key, item := range in.Items {
		if strings.TrimSpace(key) == "" || len([]rune(key)) > 100 || len([]rune(item.Note)) > 2000 {
			return ports.ErrInvalid
		}
	}
	return nil
}

func normalizeChecks(in []CheckUpdate) []CheckUpdate {
	seen := map[string]bool{}
	out := make([]CheckUpdate, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		check := in[i]
		check.Name = clamp(check.Name, 200)
		check.Detail = clamp(check.Detail, 2000)
		if check.Name == "" || seen[check.Name] {
			continue
		}
		seen[check.Name] = true
		out = append([]CheckUpdate{check}, out...)
	}
	return out
}

func validateChecks(in []CheckUpdate) error {
	if len(in) > 100 {
		return ports.ErrInvalid
	}
	for _, check := range in {
		if strings.TrimSpace(check.Name) == "" || len([]rune(check.Name)) > 200 ||
			!validStatus(check.Status) || len([]rune(check.Detail)) > 2000 {
			return ports.ErrInvalid
		}
	}
	return nil
}

func (s Store) Template(ctx context.Context) (Template, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return Template{}, err
	}
	var t Template
	var items []byte
	err = s.DB.QueryRowContext(ctx, `SELECT items,revision,updated_at,updated_by FROM review_checklist_templates WHERE team_id=$1`, p.TeamID).Scan(&items, &t.Revision, &t.UpdatedAt, &t.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{Items: append([]TemplateItem(nil), DefaultTemplate...), Custom: false}, nil
	}
	if err != nil {
		return Template{}, mapped(err)
	}
	if json.Unmarshal(items, &t.Items) != nil || len(t.Items) == 0 {
		return Template{}, ports.ErrUnavailable
	}
	t.Custom = true
	t.UpdatedAt = t.UpdatedAt.UTC()
	return t, nil
}

// SaveTemplate заменяет шаблон команды; наставник обязан принадлежать этой команде.
func (s Store) SaveTemplate(ctx context.Context, items []TemplateItem, revision int64) (Template, error) {
	p, err := principal(ctx, identity.Mentor)
	if err != nil {
		return Template{}, err
	}
	if len(items) == 0 || len(items) > 50 {
		return Template{}, ports.ErrInvalid
	}
	keys := map[string]bool{}
	for i, item := range items {
		item.Key = strings.TrimSpace(item.Key)
		item.Label = strings.TrimSpace(item.Label)
		if item.Key == "" || len([]rune(item.Key)) > 100 || item.Label == "" || len([]rune(item.Label)) > 300 || keys[item.Key] {
			return Template{}, ports.ErrInvalid
		}
		keys[item.Key] = true
		items[i] = item
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return Template{}, ports.ErrUnavailable
	}
	var t Template
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var existing sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM review_checklist_templates WHERE team_id=$1 FOR UPDATE`, p.TeamID).Scan(&existing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existing.Valid && revision != existing.Int64 {
			return ErrChanged
		}
		if existing.Valid {
			_, err = tx.ExecContext(ctx, `UPDATE review_checklist_templates SET items=$2,updated_by=$3,revision=revision+1,updated_at=clock_timestamp() WHERE team_id=$1`,
				p.TeamID, payload, p.UserID)
			t.Revision = existing.Int64 + 1
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO review_checklist_templates(team_id,items,updated_by,revision) VALUES ($1,$2,$3,1)`,
				p.TeamID, payload, p.UserID)
			t.Revision = 1
		}
		if err != nil {
			return err
		}
		t.Items, t.UpdatedBy, t.Custom = items, p.UserID, true
		t.UpdatedAt = time.Now().UTC()
		return nil
	})
	return t, mapped(err)
}

// mentorOfTx возвращает назначенного наставника стажёра в его команде.
func mentorOfTx(ctx context.Context, tx *sql.Tx, p identity.Principal) (string, error) {
	var mentorID string
	err := tx.QueryRowContext(ctx, `SELECT a.mentor_id FROM mentor_assignments a
    JOIN users m ON m.id=a.mentor_id AND m.team_id=a.team_id AND m.role='mentor'
    WHERE a.intern_id=$1 AND a.team_id=$2 FOR SHARE OF a,m`, p.UserID, p.TeamID).Scan(&mentorID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ports.ErrMentorUnassigned
	}
	return mentorID, err
}
