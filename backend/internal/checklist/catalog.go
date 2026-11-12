// Package checklist imports the versioned onboarding catalog into demo plans.
package checklist

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
)

//go:embed catalog.json
var catalogJSON []byte

type entry struct {
	SourceRow   int                   `json:"source_row"`
	Key         string                `json:"template_key"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Period      string                `json:"period"`
	Policy      string                `json:"completion_policy"`
	PolicyNote  string                `json:"policy_note,omitempty"`
	Resources   []onboarding.Resource `json:"resources"`
}

var validKey = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func load(data []byte) ([]entry, error) {
	var entries []entry
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode checklist: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF || len(entries) != 16 {
		return nil, fmt.Errorf("expected one checklist with 16 source rows")
	}
	keys := make(map[string]bool, len(entries))
	for i, item := range entries {
		if item.SourceRow != i+1 || !validKey.MatchString(item.Key) || keys[item.Key] ||
			strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Period) == "" || item.Resources == nil {
			return nil, fmt.Errorf("invalid checklist entry at row %d", i+1)
		}
		keys[item.Key] = true
		switch item.Policy {
		case "intern", "mentor", "both":
		default:
			return nil, fmt.Errorf("invalid completion policy at row %d", item.SourceRow)
		}
		for _, resource := range item.Resources {
			if strings.TrimSpace(resource.Title) == "" || (resource.URL == nil && (resource.Note == nil || strings.TrimSpace(*resource.Note) == "")) {
				return nil, fmt.Errorf("invalid resource at row %d", item.SourceRow)
			}
			if resource.URL != nil {
				link, err := url.Parse(*resource.URL)
				if err != nil || link.Scheme != "https" || link.Hostname() == "" || link.User != nil {
					return nil, fmt.Errorf("invalid resource URL at row %d", item.SourceRow)
				}
			}
		}
	}
	return entries, nil
}

func (e entry) instructions() string {
	if e.Description == "" {
		return e.Title
	}
	return e.Description
}

// SyncDemoPlansTx updates demo plans inside the seed transaction.
func SyncDemoPlansTx(ctx context.Context, tx *sql.Tx) error {
	entries, err := load(catalogJSON)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT p.id, p.user_id, p.team_id FROM onboarding_plans p
JOIN users u ON u.id=p.user_id AND u.team_id=p.team_id
WHERE u.demo_account AND u.role='intern' ORDER BY p.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type plan struct {
		id    string
		owner identity.Principal
	}
	var plans []plan
	for rows.Next() {
		p := plan{owner: identity.Principal{Role: identity.Intern}}
		if err := rows.Scan(&p.id, &p.owner.UserID, &p.owner.TeamID); err != nil {
			return err
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range plans {
		if err := onboarding.LockPlanTx(ctx, tx, p.owner); err != nil {
			return err
		}
		if err := syncPlan(ctx, tx, p.id, entries); err != nil {
			return fmt.Errorf("sync checklist for plan %s: %w", p.id, err)
		}
	}
	return nil
}

func syncPlan(ctx context.Context, tx *sql.Tx, planID string, entries []entry) error {
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1)+1 FROM onboarding_steps WHERE plan_id=$1`, planID).Scan(&position); err != nil {
		return err
	}
	var changed int64
	for _, e := range entries {
		resources, err := json.Marshal(e.Resources)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, upsert, planID+":"+e.Key, planID, position, e.Title, e.instructions(), e.Key, e.Period, e.Policy, resources)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		changed += count
		position++
	}
	if changed > 0 {
		_, err := tx.ExecContext(ctx, `UPDATE onboarding_plans SET revision=revision+1 WHERE id=$1`, planID)
		return err
	}
	return nil
}

// Refresh content in place and keep the policy of any step with progress.
// Changing that policy could invalidate a confirmation or a completed status.
const upsert = `
INSERT INTO onboarding_steps AS s
(id, plan_id, position, title, description, status, template_key, period, completion_policy, instructions, resources)
VALUES ($1,$2,$3,$4,$5,'not_started',$6,$7,$8,$5,$9::jsonb)
ON CONFLICT (plan_id, COALESCE(template_key,id)) DO UPDATE SET
    title=excluded.title, description=excluded.description, period=excluded.period,
    instructions=excluded.instructions, resources=excluded.resources,
    completion_policy=CASE WHEN s.status='not_started' THEN excluded.completion_policy ELSE s.completion_policy END
WHERE (s.title,s.description,s.period,s.instructions,s.resources)
    IS DISTINCT FROM (excluded.title,excluded.description,excluded.period,excluded.instructions,excluded.resources)
    OR (s.status='not_started' AND s.completion_policy<>excluded.completion_policy)`
