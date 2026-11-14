// Package curator manages scoped onboarding metrics, problems and template publications.
package curator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Store struct{ DB *sql.DB }
type source struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type step struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
}
type change struct {
	Action        string     `json:"action"`
	ID            string     `json:"id"`
	Team          string     `json:"team_id"`
	Kind          string     `json:"kind"`
	Direction     string     `json:"direction"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Owner         *string    `json:"owner_id"`
	Due           *time.Time `json:"due_at"`
	Resolution    string     `json:"resolution"`
	Document      *string    `json:"document_id"`
	Template      *int64     `json:"template_version"`
	Revision      int64      `json:"revision"`
	MergeFrom     string     `json:"merge_from"`
	MergeRevision int64      `json:"merge_revision"`
	Evidence      []source   `json:"evidence"`
	Steps         []step     `json:"steps"`
	Apply         bool       `json:"existing_plans"`
}

// Inputs are row identifiers and timings only. Topic identity comes from a reviewed group
// for mentor questions. HR aggregation never returns personal question text or record IDs.
const events = `SELECT a.id,'access' kind,'mentor' direction,a.user_id intern,a.resource_id topic,a.created_at at,
 EXTRACT(epoch FROM (CASE WHEN a.status IN ('pending','submitting') THEN now() ELSE a.updated_at END-a.created_at))/3600 hours
 FROM access_requests a WHERE a.team_id=$1 AND a.status IN ('pending','submitting','granted','rejected','failed')
 UNION ALL
 SELECT s.id,'blocked','mentor',p.user_id,COALESCE(s.template_key,s.title),s.meeting_changed_at,
 EXTRACT(epoch FROM (now()-s.meeting_changed_at))/3600
 FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id
 WHERE p.team_id=$1 AND s.status='blocked'
 UNION ALL
 SELECT q.id::text,'knowledge',q.direction,q.student_id,
 CASE WHEN q.direction='hr' THEN 'hr' ELSE COALESCE(grouped.group_id::text,COALESCE(s.template_key,'general')) END,
 q.created_at,NULL::numeric
 FROM knowledge_gaps q LEFT JOIN onboarding_steps s ON s.id=q.step_id
 LEFT JOIN question_group_members grouped ON grouped.question_id=q.id AND grouped.removed_at IS NULL
 WHERE q.team_id=$1`

func (s Store) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/curator", session.Require(http.HandlerFunc(s.list), false))
	mux.Handle("POST /api/v1/curator", session.Require(http.HandlerFunc(s.mutate), false))
}
func (s Store) list(w http.ResponseWriter, r *http.Request) {
	p, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	team, kind, direction, state := q.Get("team_id"), q.Get("kind"), q.Get("direction"), q.Get("status")
	until := time.Now().UTC()
	since := until.AddDate(0, 0, -30)
	for key, dest := range map[string]*time.Time{"from": &since, "to": &until} {
		if value := q.Get(key); value != "" {
			*dest, err = time.Parse(time.RFC3339, value)
			if err != nil {
				writeError(w, r, ports.ErrInvalid)
				return
			}
		}
	}
	if !since.Before(until) || until.Sub(since) > 366*24*time.Hour || !optional(kind, "access", "blocked", "knowledge") || !optional(direction, "mentor", "hr") || !optional(state, "open", "in_progress", "resolved", "merged") {
		writeError(w, r, ports.ErrInvalid)
		return
	}
	var result json.RawMessage
	err = database.WithinTx(r.Context(), s.DB, func(tx *sql.Tx) error {
		if team != "" {
			if err := checkScope(r.Context(), tx, p.UserID, team); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(r.Context(), `
 WITH teams_in_scope AS (
 SELECT t.id,t.name FROM teams t JOIN curator_team_assignments a ON a.team_id=t.id WHERE a.curator_id=$6
 ), visible_events AS (
 SELECT * FROM (`+events+`) e WHERE at >= $2 AND at < $3 AND ($4='' OR kind=$4) AND ($5='' OR direction=$5)
 AND (kind<>'access' OR hours>=24)
 ), summaries AS (
 SELECT kind,direction,topic,count(*) events,count(DISTINCT intern) interns,round(avg(hours),1) average_hours,
 count(*) FILTER (WHERE at < $2::timestamptz+($3::timestamptz-$2::timestamptz)/2) earlier_events,
 count(*) FILTER (WHERE at >= $2::timestamptz+($3::timestamptz-$2::timestamptz)/2) later_events,
 CASE WHEN direction='hr' THEN '[]'::jsonb ELSE to_jsonb((array_agg(id ORDER BY at DESC,id))[1:50]) END source_ids
 FROM visible_events GROUP BY kind,direction,topic HAVING count(DISTINCT intern)>=2
 ORDER BY count(DISTINCT intern) DESC,kind,direction,topic LIMIT 100
 ), problems AS (
 SELECT to_jsonb(p) || jsonb_build_object('evidence',COALESCE((SELECT jsonb_agg(jsonb_build_object('kind',e.source_kind,'id',e.source_id)) FROM onboarding_problem_evidence e WHERE e.problem_id=p.id),'[]'::jsonb),
 'history',COALESCE((SELECT jsonb_agg(h ORDER BY h.revision DESC) FROM (SELECT revision,actor_id,changed_at,snapshot FROM onboarding_problem_history WHERE problem_id=p.id ORDER BY revision DESC LIMIT 30) h),'[]'::jsonb)) obj
 FROM onboarding_system_problems p WHERE team_id=$1
 AND ($4='' OR kind=$4) AND ($5='' OR direction=$5) AND ($7='' OR status=$7)
 ORDER BY updated_at DESC,id LIMIT 100
 )
 SELECT jsonb_build_object('teams',COALESCE((SELECT jsonb_agg(t ORDER BY t.name,t.id) FROM teams_in_scope t),'[]'::jsonb),
 'summaries',COALESCE((SELECT jsonb_agg(s) FROM summaries s),'[]'::jsonb),
 'problems',COALESCE((SELECT jsonb_agg(obj) FROM problems),'[]'::jsonb),
 'owners',COALESCE((SELECT jsonb_agg(row_to_json(u)) FROM
 (SELECT id,name,role FROM users WHERE team_id=$1 AND role='mentor'
 UNION SELECT u.id,u.name,u.role FROM users u JOIN hr_team_assignments h ON h.hr_user_id=u.id WHERE h.team_id=$1 AND h.active AND u.role='hr') u),'[]'::jsonb),
 'template',(SELECT jsonb_build_object('version',version,'steps',steps) FROM onboarding_template_publications WHERE team_id=$1 ORDER BY version DESC LIMIT 1),
 'plan_count',(SELECT count(*) FROM onboarding_plans WHERE team_id=$1))`, team, since, until, kind, direction, p.UserID, state).Scan(&result)
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, result)
}
func (s Store) mutate(w http.ResponseWriter, r *http.Request) {
	p, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in change
	if e := httpapi.DecodeJSONLimit(w, r, &in, 65536); e != nil {
		httpapi.WriteError(w, r, e)
		return
	}
	if !valid(in) {
		writeError(w, r, ports.ErrInvalid)
		return
	}
	var version int64
	err = database.WithinTx(r.Context(), s.DB, func(tx *sql.Tx) error {
		ctx := r.Context()
		if err := checkScope(ctx, tx, p.UserID, in.Team); err != nil {
			return err
		}
		var team string
		// Serialize problem moves and publications within a team.
		if err := tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id=$1 FOR NO KEY UPDATE`, in.Team).Scan(&team); err != nil {
			return err
		}
		var err error
		if in.Action == "publish-template" {
			version, err = publish(ctx, tx, p.UserID, in)
		} else {
			version, err = saveProblem(ctx, tx, p.UserID, in)
		}
		return err
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, map[string]int64{"revision": version})
}
func checkScope(ctx context.Context, tx *sql.Tx, user, team string) error {
	var value int
	return tx.QueryRowContext(ctx, `SELECT 1 FROM curator_team_assignments WHERE curator_id=$1 AND team_id=$2 FOR SHARE`, user, team).Scan(&value)
}
func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}
func optional(v string, values ...string) bool { return v == "" || oneOf(v, values...) }
func shortText(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= max
}
func valid(in change) bool {
	if len(in.ID) != 36 || !shortText(in.Team, 128) || in.Revision < 0 {
		return false
	}
	for i, c := range in.ID {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	if in.Action == "publish-template" {
		if len(in.Steps) == 0 || len(in.Steps) > 50 {
			return false
		}
		keys := map[string]bool{}
		for _, step := range in.Steps {
			if !shortText(step.Key, 80) || !shortText(step.Title, 200) || !shortText(step.Description, 4000) || keys[step.Key] {
				return false
			}
			keys[step.Key] = true
		}
		return true
	}
	if in.Action != "save-problem" || !oneOf(in.Kind, "access", "blocked", "knowledge") || !oneOf(in.Direction, "mentor", "hr") || !oneOf(in.Status, "open", "in_progress", "resolved") ||
		(in.Kind != "knowledge" && in.Direction == "hr") || !shortText(in.Title, 200) || !shortText(in.Description, 4000) || len(in.Evidence) > 50 {
		return false
	}
	if in.Status == "resolved" && (!shortText(in.Resolution, 4000) || (in.Document == nil && in.Template == nil)) {
		return false
	}
	return len(in.Resolution) <= 16000
}
func saveProblem(ctx context.Context, tx *sql.Tx, actor string, in change) (int64, error) {
	if in.Owner != nil {
		var owner string
		err := tx.QueryRowContext(ctx, `SELECT u.id FROM users u WHERE u.id=$1 AND
  (($3='mentor' AND u.role='mentor' AND u.team_id=$2) OR
  ($3='hr' AND u.role='hr' AND EXISTS(SELECT 1 FROM hr_team_assignments h WHERE h.hr_user_id=u.id AND h.team_id=$2 AND h.active)))`, *in.Owner, in.Team, in.Direction).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ports.ErrInvalid
		}
		if err != nil {
			return 0, err
		}
	}
	if in.Document != nil {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT d.id FROM knowledge_documents d WHERE d.id=$1 AND d.team_id=$2 AND d.users='[]'::jsonb AND d.direction=$3`, *in.Document, in.Team, in.Direction).Scan(&id)
		if err != nil {
			return 0, err
		}
	}
	if in.Template != nil {
		var v int64
		if err := tx.QueryRowContext(ctx, `SELECT version FROM onboarding_template_publications WHERE team_id=$1 AND version=$2`, in.Team, *in.Template).Scan(&v); err != nil {
			return 0, err
		}
	}
	for _, e := range in.Evidence {
		if e.Kind != in.Kind || in.Direction == "hr" || !shortText(e.ID, 160) {
			return 0, ports.ErrInvalid
		}
		var ok bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM (`+events+`) e WHERE kind=$2 AND id=$3 AND direction=$4)`, in.Team, e.Kind, e.ID, in.Direction).Scan(&ok); err != nil {
			return 0, err
		}
		if !ok {
			return 0, ports.ErrInvalid
		}
	}
	var revision int64
	var err error
	if in.Revision == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO onboarding_system_problems
  (id,team_id,kind,direction,title,description,status,owner_id,due_at,resolution,document_id,template_version)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(id) DO NOTHING RETURNING revision`,
			in.ID, in.Team, in.Kind, in.Direction, in.Title, in.Description, in.Status, in.Owner, in.Due, in.Resolution, in.Document, in.Template).Scan(&revision)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE onboarding_system_problems SET title=$5,description=$6,status=$7,owner_id=$8,due_at=$9,resolution=$10,
  document_id=$11,template_version=$12,revision=revision+1,updated_at=clock_timestamp()
  WHERE id=$1 AND team_id=$2 AND kind=$3 AND direction=$4 AND revision=$13 AND status<>'merged' RETURNING revision`,
			in.ID, in.Team, in.Kind, in.Direction, in.Title, in.Description, in.Status, in.Owner, in.Due, in.Resolution, in.Document, in.Template, in.Revision).Scan(&revision)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ports.ErrConflict
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM onboarding_problem_evidence WHERE problem_id=$1`, in.ID); err != nil {
		return 0, err
	}
	for _, e := range in.Evidence {
		if _, err = tx.ExecContext(ctx, `INSERT INTO onboarding_problem_evidence VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, in.ID, e.Kind, e.ID); err != nil {
			return 0, err
		}
	}
	if in.MergeFrom != "" {
		if in.MergeFrom == in.ID || in.MergeRevision < 1 {
			return 0, ports.ErrInvalid
		}
		var rev int64
		err = tx.QueryRowContext(ctx, `UPDATE onboarding_system_problems SET status='merged',merged_into=$2,revision=revision+1,updated_at=clock_timestamp()
  WHERE id::text=$1 AND team_id=$3 AND kind=$4 AND direction=$5 AND revision=$6 AND status<>'merged' RETURNING revision`,
			in.MergeFrom, in.ID, in.Team, in.Kind, in.Direction, in.MergeRevision).Scan(&rev)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ports.ErrConflict
		}
		if err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO onboarding_problem_evidence SELECT $1,source_kind,source_id FROM onboarding_problem_evidence WHERE problem_id::text=$2 ON CONFLICT DO NOTHING`, in.ID, in.MergeFrom); err != nil {
			return 0, err
		}
		if err = history(ctx, tx, in.MergeFrom, actor); err != nil {
			return 0, err
		}
	}
	return revision, history(ctx, tx, in.ID, actor)
}
func history(ctx context.Context, tx *sql.Tx, id, actor string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO onboarding_problem_history(problem_id,revision,actor_id,snapshot)
 SELECT p.id,p.revision,$2,to_jsonb(p) FROM onboarding_system_problems p WHERE p.id=$1`, id, actor)
	return err
}
func publish(ctx context.Context, tx *sql.Tx, actor string, in change) (int64, error) {
	steps, _ := json.Marshal(in.Steps)
	var version int64
	var same bool
	err := tx.QueryRowContext(ctx, `SELECT version,steps=$4::jsonb AND existing_plans=$5 FROM onboarding_template_publications
 WHERE operation_id=$1 AND team_id=$2 AND author_id=$3`, in.ID, in.Team, actor, steps, in.Apply).Scan(&version, &same)
	if err == nil {
		if !same {
			return 0, ports.ErrConflict
		}
		return version, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM onboarding_template_publications WHERE team_id=$1`, in.Team).Scan(&version); err != nil {
		return 0, err
	}
	if version != in.Revision {
		return 0, ports.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO onboarding_template_publications(team_id,version,operation_id,author_id,steps,existing_plans)
 VALUES($1,$2,$3,$4,$5,$6)`, in.Team, version+1, in.ID, actor, steps, in.Apply); err != nil {
		return 0, err
	}
	if in.Apply {
		if _, err = tx.ExecContext(ctx, `SELECT id FROM onboarding_plans WHERE team_id=$1 ORDER BY id FOR UPDATE`, in.Team); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `SELECT apply_published_onboarding_template(id,$2::jsonb) FROM onboarding_plans WHERE team_id=$1`, in.Team, steps); err != nil {
			return 0, err
		}
	}
	return version + 1, nil
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrInvalid):
		httpapi.WriteError(w, r, httpapi.ValidationError(nil))
	case errors.Is(err, ports.ErrConflict):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "REVISION_CONFLICT", Message: "Данные изменились. Обновите страницу и повторите."})
	case errors.Is(err, sql.ErrNoRows):
		httpapi.WriteServiceError(w, r, ports.ErrNotFound)
	case errors.Is(err, identity.ErrUnauthenticated):
		httpapi.WriteServiceError(w, r, err)
	default:
		httpapi.WriteServiceError(w, r, ports.ErrUnavailable)
	}
}
