// Package meetings stores participant-only onboarding meetings and agreements.
package meetings

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Store struct{ DB *sql.DB }
type agreement struct {
	Content string     `json:"content"`
	Owner   string     `json:"owner_id"`
	Due     *time.Time `json:"due_at"`
	Step    *string    `json:"step_id"`
}
type change struct {
	ID         string      `json:"id"`
	Revision   int64       `json:"revision"`
	Intern     string      `json:"intern_id"`
	At         time.Time   `json:"scheduled_at"`
	Zone       string      `json:"timezone"`
	Action     string      `json:"action"`
	Topic      string      `json:"topic_id"`
	Content    string      `json:"content"`
	Private    bool        `json:"private"`
	Agreements []agreement `json:"agreements"`
	Due        *time.Time  `json:"due_at"`
}

const participants = `SELECT a.intern_id,a.mentor_id,a.team_id FROM mentor_assignments a
 JOIN users i ON i.id=a.intern_id AND i.team_id=a.team_id AND i.role='intern'
 JOIN users m ON m.id=a.mentor_id AND m.team_id=a.team_id AND m.role='mentor'
 WHERE a.team_id=$2 AND (($3='intern' AND a.intern_id=$1) OR ($3='mentor' AND a.mentor_id=$1))`
const visible = ` FROM onboarding_meetings m JOIN (` + participants + `) a USING(intern_id,mentor_id,team_id)`
const previousMeeting = `SELECT other.id,other.completed_at FROM onboarding_meetings other
 WHERE other.intern_id=m.intern_id AND other.mentor_id=m.mentor_id AND other.team_id=m.team_id AND other.id<>m.id
 AND other.completed_at<LEAST(m.scheduled_at,clock_timestamp())
 ORDER BY other.completed_at DESC,other.id LIMIT 1`
const meetingJSON = `jsonb_build_object('id',m.id,'intern_id',m.intern_id,'mentor_id',m.mentor_id,'previous_id',prev.id,
 'scheduled_at',m.scheduled_at,'timezone',m.timezone,'status',m.status,'revision',m.revision,'result',m.result,
 'topics',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',t.id,'author_id',t.author_id,'content',t.content,'private',t.private) ORDER BY t.id)
 FROM onboarding_meeting_topics t WHERE t.meeting_id=m.id AND (NOT t.private OR t.author_id=$1)),'[]'::jsonb),
 'agreements',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',g.id,'owner_id',g.owner_id,'content',g.content,'due_at',g.due_at,'step_id',g.step_id,'completed_at',g.completed_at,'revision',g.revision) ORDER BY g.id)
 FROM onboarding_agreements g WHERE g.meeting_id=m.id),'[]'::jsonb),
 'agenda',COALESCE((SELECT jsonb_agg(item ORDER BY priority, key) FROM (
 SELECT 1 priority,s.id key,jsonb_build_object('kind','checkpoint','text',s.title,'step_id',s.id,'state',s.status,'reason',s.blocked_reason) item
 FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id
 WHERE p.user_id=m.intern_id AND p.team_id=m.team_id AND (s.status='blocked' OR s.meeting_changed_at>COALESCE(prev.completed_at,'-infinity') AND s.meeting_changed_at<=COALESCE(m.completed_at,now()))
 UNION ALL
 SELECT 2,q.id::text,jsonb_build_object('kind','question','text',q.question,'step_id',q.step_id,'state',q.status)
 FROM knowledge_gaps q WHERE q.team_id=m.team_id AND q.student_id=m.intern_id AND q.mentor_id=m.mentor_id AND q.direction='mentor' AND q.status<>'resolved'
 AND q.created_at>COALESCE(prev.completed_at,'-infinity') AND q.created_at<=COALESCE(m.completed_at,now())
 UNION ALL
 SELECT 3,g.id::text,jsonb_build_object('kind','agreement','text',g.content,'step_id',g.step_id,'state','open')
 FROM onboarding_agreements g JOIN onboarding_meetings older ON older.id=g.meeting_id
 WHERE older.intern_id=m.intern_id AND older.mentor_id=m.mentor_id AND older.team_id=m.team_id AND older.id<>m.id
 AND older.completed_at<=COALESCE(m.completed_at,now()) AND g.completed_at IS NULL
 ORDER BY priority,key LIMIT 100
 ) items),'[]'::jsonb))`

func (s Store) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/meetings", session.Require(http.HandlerFunc(s.list), false))
	mux.Handle("POST /api/v1/meetings", session.Require(http.HandlerFunc(s.mutate), false))
}
func (s Store) list(w http.ResponseWriter, r *http.Request) {
	p, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var result json.RawMessage
	err = s.DB.QueryRowContext(r.Context(), `SELECT jsonb_build_object(
 'participants',COALESCE((SELECT jsonb_agg(row_to_json(a)) FROM (`+participants+`) a),'[]'::jsonb),
 'meetings',COALESCE((SELECT jsonb_agg(item) FROM (SELECT `+meetingJSON+` item`+visible+`
 LEFT JOIN LATERAL (`+previousMeeting+`) latest ON m.status='scheduled'
 LEFT JOIN onboarding_meetings prev ON prev.id=CASE WHEN m.status='scheduled' THEN latest.id ELSE m.previous_id END
 ORDER BY m.scheduled_at DESC,m.id LIMIT 100) meetings),'[]'::jsonb))`, p.UserID, p.TeamID, p.Role).Scan(&result)
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
	if !validChange(in) {
		writeError(w, r, ports.ErrInvalid)
		return
	}
	var revision int64
	err = database.WithinTx(r.Context(), s.DB, func(tx *sql.Tx) error {
		var e error
		revision, e = s.apply(r.Context(), tx, p, in)
		return e
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, map[string]int64{"revision": revision})
}
func validChange(in change) bool {
	if !uuid(in.ID) || len(in.Agreements) > 20 {
		return false
	}
	switch in.Action {
	case "create", "reschedule":
		_, err := time.LoadLocation(in.Zone)
		return err == nil && in.Zone != "" && in.Zone != "Local" && len(in.Zone) <= 100 && in.At.Year() >= 2000 && in.At.Year() < 2200
	case "topic":
		return uuid(in.Topic) && validContent(in.Content, 2000)
	case "complete":
		if !validContent(in.Content, 4000) {
			return false
		}
		for _, a := range in.Agreements {
			if !validContent(a.Content, 2000) {
				return false
			}
		}
		return true
	case "cancel", "agreement-complete", "agreement-reschedule":
		return true
	default:
		return false
	}
}
func uuid(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
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
	return true
}
func validContent(value string, max int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= max
}
func (s Store) apply(ctx context.Context, tx *sql.Tx, p identity.Principal, in change) (int64, error) {
	if in.Action == "create" {
		return create(ctx, tx, p, in)
	}
	if strings.HasPrefix(in.Action, "agreement-") {
		return changeAgreement(ctx, tx, p, in)
	}
	var status, intern, mentor, team string
	var finishedHash sql.NullString
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT m.status,m.revision,m.intern_id,m.mentor_id,m.team_id,m.finish_hash`+visible+` WHERE m.id=$4 FOR UPDATE OF m`, p.UserID, p.TeamID, p.Role, in.ID).Scan(&status, &revision, &intern, &mentor, &team, &finishedHash)
	if err != nil {
		return 0, err
	}
	if err = lockAssignment(ctx, tx, intern, mentor, team); err != nil {
		return 0, err
	}
	if in.Action == "complete" && status == "completed" {
		if finishedHash.String != payloadHash(in) {
			return 0, ports.ErrConflict
		}
		return revision, nil
	}
	if status != "scheduled" || in.Revision != revision {
		return 0, ports.ErrConflict
	}
	switch in.Action {
	case "reschedule":
		_, err = tx.ExecContext(ctx, `UPDATE onboarding_meetings SET scheduled_at=$2,timezone=$3,
previous_id=(SELECT other.id FROM onboarding_meetings other WHERE other.intern_id=$4 AND other.mentor_id=$5
 AND other.team_id=$6 AND other.completed_at<$2 ORDER BY other.completed_at DESC,other.id LIMIT 1)
WHERE id=$1`, in.ID, in.At, in.Zone, intern, mentor, team)
	case "cancel":
		_, err = tx.ExecContext(ctx, `UPDATE onboarding_meetings SET status='cancelled' WHERE id=$1`, in.ID)
	case "topic":
		var result sql.Result
		result, err = tx.ExecContext(ctx, `INSERT INTO onboarding_meeting_topics(id,meeting_id,author_id,content,private) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(id) DO UPDATE SET content=EXCLUDED.content,private=EXCLUDED.private
 WHERE onboarding_meeting_topics.meeting_id=EXCLUDED.meeting_id AND onboarding_meeting_topics.author_id=EXCLUDED.author_id`, in.Topic, in.ID, p.UserID, in.Content, in.Private)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				return 0, ports.ErrNotFound
			}
		}
	case "complete":
		err = complete(ctx, tx, in, intern, mentor, team)
	}
	if err != nil {
		return 0, err
	}
	return revision + 1, record(ctx, tx, in.ID, p.UserID, in.Action, revision+1)
}
func lockAssignment(ctx context.Context, tx *sql.Tx, intern, mentor, team string) error {
	var ok int
	return tx.QueryRowContext(ctx, `SELECT 1 FROM mentor_assignments a JOIN users i ON i.id=a.intern_id AND i.role='intern'
 JOIN users m ON m.id=a.mentor_id AND m.role='mentor' WHERE a.intern_id=$1 AND a.mentor_id=$2 AND a.team_id=$3 FOR SHARE OF a,i,m`, intern, mentor, team).Scan(&ok)
}
func create(ctx context.Context, tx *sql.Tx, p identity.Principal, in change) (int64, error) {
	intern := in.Intern
	if p.Role == identity.Intern {
		intern = p.UserID
	}
	var mentor, team string
	if err := tx.QueryRowContext(ctx, `SELECT mentor_id,team_id FROM (`+participants+`) a WHERE intern_id=$4`, p.UserID, p.TeamID, p.Role, intern).Scan(&mentor, &team); err != nil {
		return 0, err
	}
	if err := lockAssignment(ctx, tx, intern, mentor, team); err != nil {
		return 0, err
	}
	hash := payloadHash(in)
	result, err := tx.ExecContext(ctx, `INSERT INTO onboarding_meetings(id,intern_id,mentor_id,team_id,previous_id,scheduled_at,timezone,created_by,create_hash)
 VALUES($1,$2,$3,$4,(SELECT id FROM onboarding_meetings WHERE intern_id=$2 AND mentor_id=$3 AND team_id=$4 AND status='completed'
 AND completed_at<$5 ORDER BY completed_at DESC,id LIMIT 1),$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, in.ID, intern, mentor, team, in.At, in.Zone, p.UserID, hash)
	if err != nil {
		return 0, err
	}
	if count, _ := result.RowsAffected(); count == 1 {
		return 1, record(ctx, tx, in.ID, p.UserID, "create", 1)
	}
	var previous string
	var rev int64
	err = tx.QueryRowContext(ctx, `SELECT m.create_hash,m.revision`+visible+` WHERE m.id=$4 AND m.created_by=$1`, p.UserID, p.TeamID, p.Role, in.ID).Scan(&previous, &rev)
	if err == nil && previous != hash {
		return 0, ports.ErrConflict
	}
	return rev, err
}
func payloadHash(in change) string {
	payload, _ := json.Marshal(in)
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}
func record(ctx context.Context, tx *sql.Tx, id, actor, action string, revision int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE onboarding_meetings SET revision=$2 WHERE id=$1`, id, revision)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO onboarding_meeting_history(meeting_id,revision,actor_id,action) VALUES($1,$2,$3,$4)`, id, revision, actor, action)
	return err
}
func complete(ctx context.Context, tx *sql.Tx, in change, intern, mentor, team string) error {
	for _, a := range in.Agreements {
		if a.Owner != intern && a.Owner != mentor {
			return ports.ErrInvalid
		}
		if a.Step != nil {
			var owned bool
			err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id WHERE s.id=$1 AND p.user_id=$2 AND p.team_id=$3)`, *a.Step, intern, team).Scan(&owned)
			if err != nil {
				return err
			}
			if !owned {
				return ports.ErrInvalid
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO onboarding_agreements(meeting_id,owner_id,content,due_at,step_id) VALUES($1,$2,$3,$4,$5)`, in.ID, a.Owner, a.Content, a.Due, a.Step); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE onboarding_meetings m SET status='completed',result=$2,completed_at=clock_timestamp(),finish_hash=$3,
 previous_id=(SELECT previous.id FROM (`+previousMeeting+`) previous) WHERE m.id=$1`, in.ID, in.Content, payloadHash(in))
	return err
}
func changeAgreement(ctx context.Context, tx *sql.Tx, p identity.Principal, in change) (int64, error) {
	var revision int64
	var done bool
	var intern, mentor, team string
	err := tx.QueryRowContext(ctx, `SELECT g.revision,g.completed_at IS NOT NULL,m.intern_id,m.mentor_id,m.team_id`+visible+`
 JOIN onboarding_agreements g ON g.meeting_id=m.id WHERE g.id=$4 AND g.owner_id=$1 FOR UPDATE OF g`, p.UserID, p.TeamID, p.Role, in.ID).Scan(&revision, &done, &intern, &mentor, &team)
	if err != nil {
		return 0, err
	}
	if err = lockAssignment(ctx, tx, intern, mentor, team); err != nil {
		return 0, err
	}
	if done && in.Action == "agreement-complete" {
		return revision, nil
	}
	if done || in.Revision != revision {
		return 0, ports.ErrConflict
	}
	if in.Action == "agreement-complete" {
		_, err = tx.ExecContext(ctx, `UPDATE onboarding_agreements SET completed_at=clock_timestamp(),revision=revision+1 WHERE id=$1`, in.ID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE onboarding_agreements SET due_at=$2,revision=revision+1 WHERE id=$1`, in.ID, in.Due)
	}
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO onboarding_agreement_history(agreement_id,revision,actor_id,action) VALUES($1,$2,$3,$4)`, in.ID, revision+1, p.UserID, in.Action)
	return revision + 1, err
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrInvalid):
		httpapi.WriteError(w, r, httpapi.ValidationError(nil))
	case errors.Is(err, ports.ErrConflict):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "REVISION_CONFLICT", Message: "Данные изменились. Обновите страницу и повторите."})
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, ports.ErrNotFound):
		httpapi.WriteServiceError(w, r, ports.ErrNotFound)
	case errors.Is(err, identity.ErrUnauthenticated):
		httpapi.WriteServiceError(w, r, err)
	default:
		httpapi.WriteServiceError(w, r, ports.ErrUnavailable)
	}
}
