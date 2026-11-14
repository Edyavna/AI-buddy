package knowledgegaps

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var (
	ErrHRUnassigned = errors.New("no HR for team")
	ErrClaimed      = errors.New("question assigned to another HR")
	idPattern       = regexp.MustCompile(`(?i)^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
)

type Draft struct {
	Direction       string        `json:"direction"`
	Question        string        `json:"question"`
	Context         string        `json:"context_excerpt"`
	ContextSnapshot *ContextInput `json:"context_snapshot,omitempty"`
}

type Operation struct {
	Draft
	ID       string `json:"operation_id"`
	Revision int64  `json:"revision"`
}

type HRUpdate struct {
	ID         string   `json:"operation_id"`
	Revision   int64    `json:"revision"`
	Action     string   `json:"action"`
	Resolution string   `json:"resolution"`
	Documents  []string `json:"document_ids"`
}

type MessageQuestions struct {
	Items     []Question               `json:"items"`
	Drafts    []Draft                  `json:"drafts"`
	Question  string                   `json:"question"`
	Available []string                 `json:"available_directions"`
	Routing   *ports.RoutingSuggestion `json:"routing"`
}

type HistoryEntry struct {
	Revision int64           `json:"revision"`
	Snapshot json.RawMessage `json:"snapshot"`
}

type History struct {
	Question Question       `json:"question"`
	Entries  []HistoryEntry `json:"history"`
}

func validDraft(d Draft) bool {
	return (d.Direction == "hr" || d.Direction == "mentor") &&
		strings.TrimSpace(d.Question) != "" && utf8.RuneCountInString(d.Question) <= 8000 &&
		utf8.RuneCountInString(d.Context) <= 8000 && !strings.ContainsRune(d.Question+d.Context, 0) && validContext(d.ContextSnapshot)
}

func sourceMessage(ctx context.Context, tx *sql.Tx, p identity.Principal, messageID string, lock bool) (question string, step *string, routing []byte, err error) {
	query := `SELECT u.text,c.step_id,a.routing FROM chat_messages a
JOIN conversations c ON c.id=a.conversation_id
JOIN chat_messages u ON u.id=a.in_reply_to_message_id AND u.conversation_id=c.id AND u.role='user'
WHERE a.id=$1 AND a.role='assistant' AND c.user_id=$2 AND c.team_id=$3`
	if lock {
		query += " FOR UPDATE OF a"
	}
	err = tx.QueryRowContext(ctx, query, messageID, p.UserID, p.TeamID).Scan(&question, &step, &routing)
	return
}

func (s Store) MessageQuestions(ctx context.Context, id string) (MessageQuestions, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return MessageQuestions{}, err
	}
	out := MessageQuestions{Items: []Question{}, Drafts: []Draft{}, Available: []string{}}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var routing []byte
		out.Question, _, routing, err = sourceMessage(ctx, tx, p, id, false)
		if err != nil {
			return err
		}
		if len(routing) > 0 {
			if err := json.Unmarshal(routing, &out.Routing); err != nil {
				return err
			}
		}
		var mentor, hr bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mentor_assignments a JOIN users u ON u.id=a.mentor_id AND u.role='mentor' WHERE a.intern_id=$1 AND a.team_id=$2),
EXISTS(SELECT 1 FROM hr_team_assignments h JOIN users u ON u.id=h.hr_user_id AND u.role='hr' WHERE h.team_id=$2 AND h.active)`, p.UserID, p.TeamID).Scan(&mentor, &hr); err != nil {
			return err
		}
		if mentor {
			out.Available = append(out.Available, "mentor")
		}
		if hr {
			out.Available = append(out.Available, "hr")
		}
		out.Items, err = queryQuestions(ctx, tx, `WHERE q.message_id=$1 AND q.student_id=$2 AND q.team_id=$3 ORDER BY q.created_at,q.id`, id, p.UserID, p.TeamID)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT direction,question,context_excerpt,context_snapshot FROM question_drafts WHERE message_id=$1 ORDER BY direction`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var d Draft
			var snapshot []byte
			if err := rows.Scan(&d.Direction, &d.Question, &d.Context, &snapshot); err != nil {
				rows.Close()
				return err
			}
			if len(snapshot) > 0 {
				if err := json.Unmarshal(snapshot, &d.ContextSnapshot); err != nil {
					rows.Close()
					return err
				}
			}
			out.Drafts = append(out.Drafts, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for i := range out.Drafts {
			if err = sanitizeContextDraft(ctx, tx, p, id, &out.Drafts[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		for i := range out.Items {
			if err = s.filterLinks(ctx, p, &out.Items[i]); err != nil {
				break
			}
		}
	}
	return out, mapped(err)
}

type questionsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryQuestions(ctx context.Context, db questionsQueryer, where string, args ...any) ([]Question, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+columns+joined+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Question{}
	for rows.Next() {
		q, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func saveDraft(ctx context.Context, tx *sql.Tx, id string, d Draft) error {
	raw, err := json.Marshal(d.ContextSnapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO question_drafts(message_id,direction,question,context_excerpt,context_snapshot)
VALUES($1,$2,$3,$4,$5) ON CONFLICT(message_id,direction) DO UPDATE SET question=EXCLUDED.question,context_excerpt=EXCLUDED.context_excerpt,context_snapshot=EXCLUDED.context_snapshot`, id, d.Direction, d.Question, d.Context, string(raw))
	return err
}

func (s Store) SaveDraft(ctx context.Context, id string, d Draft) (Draft, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return d, err
	}
	if !validDraft(d) {
		return d, ports.ErrInvalid
	}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := hrscope.LockTeam(ctx, tx, p.TeamID); err != nil {
			return err
		}
		if _, _, _, err := sourceMessage(ctx, tx, p, id, true); err != nil {
			return err
		}
		if _, err := buildContext(ctx, tx, p, p, id, d.Direction, d.ContextSnapshot); err != nil {
			return err
		}
		return saveDraft(ctx, tx, id, d)
	})
	return d, mapped(err)
}

func destination(ctx context.Context, tx *sql.Tx, team, student, direction string) (*string, error) {
	if direction == "hr" {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT h.hr_user_id FROM hr_team_assignments h JOIN users u ON u.id=h.hr_user_id AND u.role='hr'
WHERE h.team_id=$1 AND h.active ORDER BY h.hr_user_id LIMIT 1 FOR SHARE OF h,u`, team).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrHRUnassigned
		}
		return nil, err
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT a.mentor_id FROM mentor_assignments a JOIN users u ON u.id=a.mentor_id AND u.role='mentor' AND u.team_id=a.team_id
WHERE a.team_id=$1 AND a.intern_id=$2 FOR SHARE OF a,u`, team, student).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ports.ErrMentorUnassigned
	}
	return &id, err
}

func operationHash(ctx context.Context, tx *sql.Tx, actor, key, kind, target string, input any) (string, error) {
	if !idPattern.MatchString(key) {
		return "", ports.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(628114,hashtext($1::text || ':' || lower($2::text)))`, actor, key); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(append([]byte(kind+":"+target+":"), encoded...))), nil
}

func replay(ctx context.Context, tx *sql.Tx, actor, key, hash string) (id string, revision int64, err error) {
	var old string
	err = tx.QueryRowContext(ctx, `SELECT question_id::text,applied_revision,payload_hash FROM question_operations WHERE actor_id=$1 AND operation_id=$2`, actor, key).Scan(&id, &revision, &old)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	if err == nil && old != hash {
		err = ports.ErrConflict
	}
	return
}

func record(ctx context.Context, tx *sql.Tx, actor, key, hash string, q *Question) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO question_operations(actor_id,operation_id,payload_hash,question_id,applied_revision) VALUES($1,$2,$3,$4,$5)`, actor, key, hash, q.ID, q.Revision)
	q.AppliedRevision = &q.Revision
	return err
}

const ownQuestion = `WHERE q.id::text=$1 AND q.student_id=$2 AND q.team_id=$3`
const hrQuestion = `q.direction='hr' AND EXISTS(SELECT 1 FROM hr_team_assignments h WHERE h.hr_user_id=$1 AND h.team_id=q.team_id AND h.active)`

func (s Store) Create(ctx context.Context, id string, in Operation) (Question, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return Question{}, err
	}
	if !validDraft(in.Draft) || in.ContextSnapshot != nil && !in.ContextSnapshot.Reviewed {
		return Question{}, ports.ErrInvalid
	}
	var q Question
	var noDestination error
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		hash, err := operationHash(ctx, tx, p.UserID, in.ID, "create", id, in)
		if err != nil {
			return err
		}
		// Drafts and context reads acquire the team lock before message rows.
		// Keep the same order so a concurrent save cannot deadlock with send.
		if err := hrscope.LockTeam(ctx, tx, p.TeamID); err != nil {
			return err
		}
		_, step, routing, err := sourceMessage(ctx, tx, p, id, true)
		if err != nil {
			return err
		}
		old, rev, err := replay(ctx, tx, p.UserID, in.ID, hash)
		if err != nil {
			return err
		}
		if old != "" {
			q, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+joined+ownQuestion, old, p.UserID, p.TeamID))
			q.AppliedRevision = &rev
			return err
		}
		if _, err := buildContext(ctx, tx, p, p, id, in.Direction, in.ContextSnapshot); err != nil {
			return err
		}
		mentor, err := destination(ctx, tx, p.TeamID, p.UserID, in.Direction)
		if errors.Is(err, ErrHRUnassigned) || errors.Is(err, ports.ErrMentorUnassigned) {
			noDestination = err
			return saveDraft(ctx, tx, id, in.Draft)
		}
		if err != nil {
			return err
		}
		viewer := p
		if mentor != nil {
			viewer = identity.Principal{UserID: *mentor, TeamID: p.TeamID, Role: identity.Mentor}
		}
		approved, err := buildContext(ctx, tx, p, viewer, id, in.Direction, in.ContextSnapshot)
		if err != nil {
			return err
		}
		if in.ContextSnapshot != nil && !in.ContextSnapshot.IncludeStep {
			step = nil
		}
		contextJSON, err := json.Marshal(approved)
		if err != nil {
			return err
		}
		var questionID string
		err = tx.QueryRowContext(ctx, `INSERT INTO knowledge_gaps(message_id,student_id,team_id,step_id,direction,original_direction,mentor_id,question,context_excerpt,routing,context_snapshot)
VALUES($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,$10) ON CONFLICT(message_id,original_direction) DO NOTHING RETURNING id::text`, id, p.UserID, p.TeamID, step, in.Direction, mentor, in.Question, in.Context, routing, string(contextJSON)).Scan(&questionID)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.ErrConflict
		}
		if err != nil {
			return err
		}
		q, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+joined+ownQuestion, questionID, p.UserID, p.TeamID))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM question_drafts WHERE message_id=$1 AND direction=$2`, id, in.Direction); err != nil {
			return err
		}
		return record(ctx, tx, p.UserID, in.ID, hash, &q)
	})
	if err == nil {
		err = noDestination
	}
	if err == nil {
		err = s.filterLinks(ctx, p, &q)
	}
	return q, mapped(err)
}

func snapshot(ctx context.Context, tx *sql.Tx, q Question) error {
	raw, err := json.Marshal(q)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_gap_history(question_id,revision,snapshot) VALUES($1,$2,$3)`, q.ID, q.Revision, raw)
	return err
}

func (s Store) Reroute(ctx context.Context, id string, in Operation) (Question, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return Question{}, err
	}
	if !validDraft(in.Draft) || in.Revision < 1 || in.ContextSnapshot != nil && !in.ContextSnapshot.Reviewed {
		return Question{}, ports.ErrInvalid
	}
	var q Question
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		hash, err := operationHash(ctx, tx, p.UserID, in.ID, "reroute", id, in)
		if err != nil {
			return err
		}
		if err := hrscope.LockTeam(ctx, tx, p.TeamID); err != nil {
			return err
		}
		q, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+joined+ownQuestion+" FOR UPDATE OF q", id, p.UserID, p.TeamID))
		if err != nil {
			return err
		}
		old, rev, err := replay(ctx, tx, p.UserID, in.ID, hash)
		if err != nil {
			return err
		}
		if old != "" {
			q.AppliedRevision = &rev
			return nil
		}
		if q.Revision != in.Revision {
			return ErrChanged
		}
		if q.Direction == in.Direction {
			return ErrTransition
		}
		mentor, err := destination(ctx, tx, q.TeamID, q.Student.ID, in.Direction)
		if err != nil {
			return err
		}
		viewer := p
		if mentor != nil {
			viewer = identity.Principal{UserID: *mentor, TeamID: p.TeamID, Role: identity.Mentor}
		}
		approved, err := buildContext(ctx, tx, p, viewer, q.MessageID, in.Direction, in.ContextSnapshot)
		if err != nil {
			return err
		}
		step := q.StepID
		if in.ContextSnapshot != nil {
			step = nil
			if approved.Step != nil {
				step = &approved.Step.ID
			}
		}
		contextJSON, err := json.Marshal(approved)
		if err != nil {
			return err
		}
		if err := snapshot(ctx, tx, q); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE knowledge_gaps SET direction=$2,mentor_id=$3,hr_id=NULL,status='new',
question=$4,context_excerpt=$5,context_snapshot=$6,step_id=$7,revision=revision+1,resolution=NULL,document_ids='[]',resolved_at=NULL,updated_at=now() WHERE id=$1`, id, in.Direction, mentor, in.Question, in.Context, string(contextJSON), step); err != nil {
			return err
		}
		q, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+joined+ownQuestion, id, p.UserID, p.TeamID))
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM question_drafts WHERE message_id=$1 AND direction=$2`, q.MessageID, in.Direction); err != nil {
			return err
		}
		return record(ctx, tx, p.UserID, in.ID, hash, &q)
	})
	if err == nil {
		err = s.filterLinks(ctx, p, &q)
	}
	return q, mapped(err)
}

func (s Store) History(ctx context.Context, id string) (History, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return History{}, err
	}
	out := History{Entries: []HistoryEntry{}}
	out.Question, err = scan(s.DB.QueryRowContext(ctx, "SELECT "+columns+joined+ownQuestion, id, p.UserID, p.TeamID))
	if err != nil {
		return out, mapped(err)
	}
	if err := s.filterLinks(ctx, p, &out.Question); err != nil {
		return out, mapped(err)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT h.revision,h.snapshot FROM knowledge_gap_history h JOIN knowledge_gaps q ON q.id=h.question_id
WHERE q.id::text=$1 AND q.student_id=$2 AND q.team_id=$3 ORDER BY h.revision`, id, p.UserID, p.TeamID)
	if err != nil {
		return out, mapped(err)
	}
	defer rows.Close()
	for rows.Next() {
		var entry HistoryEntry
		if err := rows.Scan(&entry.Revision, &entry.Snapshot); err != nil {
			return out, mapped(err)
		}
		// History keeps the original links for audit. The API exposes only current authorized links.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry.Snapshot, &fields); err != nil {
			return out, mapped(err)
		}
		delete(fields, "document_ids")
		delete(fields, "context_snapshot")
		entry.Snapshot, err = json.Marshal(fields)
		if err != nil {
			return out, mapped(err)
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, mapped(rows.Err())
}

func (s Store) HRList(ctx context.Context, team, status, search string) ([]Question, error) {
	p, err := principal(ctx, identity.HR)
	if err != nil {
		return nil, err
	}
	if (status != "" && !validStatus(status)) || len(search) > 1000 {
		return nil, ports.ErrInvalid
	}
	if team != "" {
		var exists bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM hr_team_assignments WHERE hr_user_id=$1 AND team_id=$2 AND active)`, p.UserID, team).Scan(&exists); err != nil {
			return nil, mapped(err)
		}
		if !exists {
			return nil, ports.ErrNotFound
		}
	}
	qs, err := queryQuestions(ctx, s.DB, "WHERE "+hrQuestion+` AND ($2='' OR q.team_id=$2) AND ($3='' OR q.status=$3)
AND ($4='' OR strpos(lower(q.question),lower($4))>0 OR strpos(lower(u.name),lower($4))>0) ORDER BY q.created_at DESC,q.id`, p.UserID, team, status, search)
	if err == nil {
		for i := range qs {
			qs[i] = specialistView(qs[i])
			if err = s.filterLinks(ctx, p, &qs[i]); err != nil {
				break
			}
		}
	}
	return qs, mapped(err)
}

func (s Store) HRGet(ctx context.Context, id string) (Question, error) {
	p, err := principal(ctx, identity.HR)
	if err != nil {
		return Question{}, err
	}
	q, err := scan(s.DB.QueryRowContext(ctx, "SELECT "+columns+joined+"WHERE "+hrQuestion+" AND q.id::text=$2", p.UserID, id))
	if err == nil {
		err = s.filterLinks(ctx, p, &q)
	}
	return specialistView(q), mapped(err)
}

func (s Store) HRPatch(ctx context.Context, id string, in HRUpdate) (Question, error) {
	p, err := principal(ctx, identity.HR)
	if err != nil {
		return Question{}, err
	}
	if in.Revision < 1 || utf8.RuneCountInString(in.Resolution) > 8000 || strings.ContainsRune(in.Resolution, 0) || len(in.Documents) > 20 {
		return Question{}, ports.ErrInvalid
	}
	var q Question
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		hash, err := operationHash(ctx, tx, p.UserID, in.ID, "hr-patch", id, in)
		if err != nil {
			return err
		}
		var team string
		if err := tx.QueryRowContext(ctx, "SELECT q.team_id FROM knowledge_gaps q WHERE "+hrQuestion+" AND q.id::text=$2", p.UserID, id).Scan(&team); err != nil {
			return err
		}
		if err := hrscope.LockTeam(ctx, tx, team); err != nil {
			return err
		}
		if err := hrscope.CheckTeam(ctx, tx, p.UserID, team); err != nil {
			return err
		}
		q, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+joined+"WHERE "+hrQuestion+" AND q.id::text=$2 FOR UPDATE OF q", p.UserID, id))
		if err != nil {
			return err
		}
		old, rev, err := replay(ctx, tx, p.UserID, in.ID, hash)
		if err != nil {
			return err
		}
		if old != "" {
			q.AppliedRevision = &rev
			return nil
		}
		if q.SpecialistID != nil && *q.SpecialistID != p.UserID {
			// A resolved case may be reopened by a current HR after its previous specialist leaves.
			if in.Action != "reopen" || q.State != "resolved" {
				return ErrClaimed
			}
			if err := hrscope.CheckTeam(ctx, tx, *q.SpecialistID, team); !errors.Is(err, sql.ErrNoRows) {
				if err != nil {
					return err
				}
				return ErrClaimed
			}
		}
		if q.Revision != in.Revision {
			return ErrChanged
		}
		state, resolution, docs := q.State, q.Resolution, q.DocumentIDs
		switch in.Action {
		case "claim":
			if state != "new" {
				return ErrTransition
			}
			state = "in_progress"
		case "reopen":
			if state != "resolved" {
				return ErrTransition
			}
			state = "in_progress"
		case "answer", "resolve":
			if state != "in_progress" || q.SpecialistID == nil {
				return ErrTransition
			}
			value := strings.TrimSpace(in.Resolution)
			if value == "" {
				return ports.ErrInvalid
			}
			resolution = &value
			docs = in.Documents
			if docs == nil {
				docs = []string{}
			}
			if len(docs) > 0 {
				if s.Documents == nil {
					return ports.ErrUnavailable
				}
				student := identity.Principal{UserID: q.Student.ID, TeamID: q.TeamID, Role: identity.Intern}
				if err := s.Documents.CheckReady(ctx, tx, p, student, docs); err != nil {
					if errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrForbidden) {
						return ErrDocument
					}
					return err
				}
			}
			if in.Action == "resolve" {
				state = "resolved"
			}
		default:
			return ports.ErrInvalid
		}
		if err := snapshot(ctx, tx, q); err != nil {
			return err
		}
		links, err := json.Marshal(docs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE knowledge_gaps SET hr_id=$2,status=$3,resolution=$4,document_ids=$5,
revision=revision+1,updated_at=now(),resolved_at=CASE WHEN $3='resolved' THEN now() ELSE NULL END WHERE id=$1`, id, p.UserID, state, resolution, links); err != nil {
			return err
		}
		q, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+joined+"WHERE "+hrQuestion+" AND q.id::text=$2", p.UserID, id))
		if err != nil {
			return err
		}
		return record(ctx, tx, p.UserID, in.ID, hash, &q)
	})
	if err == nil {
		err = s.filterLinks(ctx, p, &q)
	}
	return specialistView(q), mapped(err)
}

func (s Store) filterLinks(ctx context.Context, p identity.Principal, q *Question) error {
	if err := s.filterContext(ctx, p, q); err != nil {
		return err
	}
	if s.Knowledge == nil || len(q.DocumentIDs) == 0 {
		return nil
	}
	allowed := make([]string, 0, len(q.DocumentIDs))
	for _, id := range q.DocumentIDs {
		if _, err := s.Knowledge.Read(identity.WithPrincipal(ctx, p), id); err != nil {
			if errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrForbidden) {
				continue
			}
			return err
		}
		allowed = append(allowed, id)
	}
	q.DocumentIDs = allowed
	return nil
}
