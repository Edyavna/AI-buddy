package knowledgegaps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var (
	ErrChanged    = errors.New("question changed")
	ErrDocument   = errors.New("document unavailable or not ready")
	ErrOutcome    = errors.New("message cannot be escalated")
	ErrTransition = errors.New("question transition forbidden")
)

// Documents must verify readiness and visibility to both principals in tx,
// keeping that decision valid until the caller commits.
type Documents interface {
	CheckReady(context.Context, *sql.Tx, identity.Principal, identity.Principal, []string) error
}

type Store struct {
	DB        *sql.DB
	Documents Documents
	Knowledge ports.Knowledge
}

type Student struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Status struct {
	ID          string    `json:"id"`
	MessageID   string    `json:"message_id"`
	State       string    `json:"status"`
	Revision    int64     `json:"revision"`
	Resolution  *string   `json:"resolution"`
	DocumentIDs []string  `json:"document_ids"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Question struct {
	Status
	TeamID            string                   `json:"team_id"`
	Direction         string                   `json:"direction"`
	OriginalDirection string                   `json:"original_direction"`
	SpecialistID      *string                  `json:"specialist_id"`
	Routing           *ports.RoutingSuggestion `json:"routing"`
	AppliedRevision   *int64                   `json:"applied_revision"`
	Student           Student                  `json:"student"`
	StepID            *string                  `json:"step_id"`
	Question          string                   `json:"question"`
	ContextExcerpt    string                   `json:"context_excerpt"`
}

type Update struct {
	State       string    `json:"status"`
	Revision    int64     `json:"revision"`
	Resolution  *string   `json:"resolution"`
	DocumentIDs *[]string `json:"document_ids"`
}

const columns = `q.id::text,q.message_id,q.status,q.revision,q.resolution,q.document_ids,q.created_at,q.updated_at,
 q.student_id,u.name,q.step_id,q.question,q.context_excerpt,q.team_id,q.direction,q.original_direction,CASE WHEN q.direction='hr' THEN q.hr_id ELSE (SELECT a.mentor_id FROM mentor_assignments a WHERE a.intern_id=q.student_id AND a.team_id=q.team_id) END,q.routing`
const joined = ` FROM knowledge_gaps q JOIN users u ON u.id=q.student_id AND u.team_id=q.team_id `
const assigned = ` q.direction='mentor' AND q.team_id=$2 AND EXISTS (
 SELECT 1 FROM mentor_assignments a WHERE a.intern_id=q.student_id AND a.mentor_id=$1 AND a.team_id=q.team_id)`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Question, error) {
	var q Question
	var documents, routing []byte
	err := row.Scan(&q.ID, &q.MessageID, &q.State, &q.Revision, &q.Resolution, &documents, &q.CreatedAt, &q.UpdatedAt,
		&q.Student.ID, &q.Student.Name, &q.StepID, &q.Question, &q.ContextExcerpt, &q.TeamID, &q.Direction, &q.OriginalDirection, &q.SpecialistID, &routing)
	if err != nil {
		return q, err
	}
	q.CreatedAt, q.UpdatedAt = q.CreatedAt.UTC(), q.UpdatedAt.UTC()
	if len(routing) > 0 {
		if err := json.Unmarshal(routing, &q.Routing); err != nil {
			return q, err
		}
	}
	return q, json.Unmarshal(documents, &q.DocumentIDs)
}

func principal(ctx context.Context, role identity.Role) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err == nil && p.Role != role {
		err = ports.ErrForbidden
	}
	return p, err
}

func mapped(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ports.ErrNotFound
	case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrInvalid), errors.Is(err, ports.ErrMentorUnassigned),
		errors.Is(err, ErrHRUnassigned), errors.Is(err, ErrClaimed), errors.Is(err, ports.ErrConflict), errors.Is(err, ErrChanged), errors.Is(err, ErrDocument), errors.Is(err, ErrOutcome), errors.Is(err, ErrTransition):
		return err
	default:
		return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
	}
}

func (s Store) Intern(ctx context.Context, messageID string, create bool) (*Status, bool, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return nil, false, err
	}
	var result *Status
	var created bool
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var messageStatus, outcome, question string
		var stepID *string
		err := tx.QueryRowContext(ctx, `SELECT a.status,COALESCE(a.outcome,''),u.text,c.step_id
    FROM chat_messages a JOIN conversations c ON c.id=a.conversation_id
    JOIN chat_messages u ON u.id=a.in_reply_to_message_id AND u.conversation_id=c.id AND u.role='user'
    WHERE a.id=$1 AND a.role='assistant' AND c.user_id=$2 AND c.team_id=$3 FOR UPDATE OF a`,
			messageID, p.UserID, p.TeamID).Scan(&messageStatus, &outcome, &question, &stepID)
		if err != nil {
			return err
		}
		q, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+joined+`WHERE q.message_id=$1 AND q.original_direction='mentor' AND q.direction='mentor' AND q.student_id=$2 AND q.team_id=$3`, messageID, p.UserID, p.TeamID))
		if err == nil {
			result = &q.Status
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !create {
			return nil
		}
		var already bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_gaps WHERE message_id=$1 AND original_direction='mentor')`, messageID).Scan(&already); err != nil {
			return err
		}
		if already {
			return ports.ErrConflict
		}
		if messageStatus != "completed" || outcome != string(ports.OutcomeKnowledgeGap) {
			return ErrOutcome
		}
		var mentorID string
		err = tx.QueryRowContext(ctx, `SELECT a.mentor_id FROM mentor_assignments a
    JOIN users m ON m.id=a.mentor_id AND m.team_id=a.team_id AND m.role='mentor'
    WHERE a.intern_id=$1 AND a.team_id=$2 FOR SHARE OF a,m`, p.UserID, p.TeamID).Scan(&mentorID)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.ErrMentorUnassigned
		}
		if err != nil {
			return err
		}
		// The excerpt uses only the linked question, never unrelated chat history.
		excerpt := []rune(strings.TrimSpace(question))
		if len(excerpt) > 240 {
			excerpt = excerpt[:240]
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO knowledge_gaps
    (message_id,student_id,team_id,mentor_id,step_id,question,context_excerpt) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			messageID, p.UserID, p.TeamID, mentorID, stepID, question, string(excerpt)); err != nil {
			return err
		}
		q, err = scan(tx.QueryRowContext(ctx, `SELECT `+columns+joined+`WHERE q.message_id=$1 AND q.original_direction='mentor'`, messageID))
		if err != nil {
			return err
		}
		result, created = &q.Status, true
		return nil
	})
	return result, created, mapped(err)
}

func (s Store) List(ctx context.Context, status string) ([]Question, error) {
	p, err := principal(ctx, identity.Mentor)
	if err != nil {
		return nil, err
	}
	if status != "" && !validStatus(status) {
		return nil, ports.ErrInvalid
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+columns+joined+`WHERE `+assigned+` AND ($3='' OR q.status=$3) ORDER BY q.created_at DESC,q.id`, p.UserID, p.TeamID, status)
	if err != nil {
		return nil, mapped(err)
	}
	defer rows.Close()
	result := []Question{}
	for rows.Next() {
		q, err := scan(rows)
		if err != nil {
			return nil, mapped(err)
		}
		result = append(result, q)
	}
	return result, mapped(rows.Err())
}

func (s Store) Get(ctx context.Context, id string) (Question, error) {
	p, err := principal(ctx, identity.Mentor)
	if err != nil {
		return Question{}, err
	}
	q, err := scan(s.DB.QueryRowContext(ctx, `SELECT `+columns+joined+`WHERE `+assigned+` AND q.id::text=$3`, p.UserID, p.TeamID, id))
	return q, mapped(err)
}

func (s Store) CountOpen(ctx context.Context, studentID string) (int, error) {
	p, err := principal(ctx, identity.Mentor)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM knowledge_gaps q WHERE q.team_id=$2
    AND q.student_id=$3 AND q.direction='mentor' AND q.status<>'resolved')
   FROM mentor_assignments a WHERE a.mentor_id=$1 AND a.team_id=$2 AND a.intern_id=$3`, p.UserID, p.TeamID, studentID).Scan(&count)
	return count, mapped(err)
}

func validStatus(status string) bool {
	return status == "new" || status == "in_progress" || status == "resolved"
}

func (s Store) Patch(ctx context.Context, id string, in Update) (Question, error) {
	p, err := principal(ctx, identity.Mentor)
	if err != nil {
		return Question{}, err
	}
	if !validStatus(in.State) || in.Revision < 1 {
		return Question{}, ports.ErrInvalid
	}
	var result Question
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		q, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+joined+`WHERE `+assigned+` AND q.id::text=$3 FOR UPDATE OF q`, p.UserID, p.TeamID, id))
		if err != nil {
			return err
		}
		var assignment int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM mentor_assignments WHERE intern_id=$1 AND team_id=$2 AND mentor_id=$3 FOR SHARE`,
			q.Student.ID, p.TeamID, p.UserID).Scan(&assignment); err != nil {
			return err
		}
		if q.Revision != in.Revision {
			return ErrChanged
		}
		from := q.State
		resolution := q.Resolution
		if in.Resolution != nil {
			value := strings.TrimSpace(*in.Resolution)
			resolution = &value
		}
		documents := q.DocumentIDs
		if in.DocumentIDs != nil {
			documents = []string{}
			for _, doc := range *in.DocumentIDs {
				if strings.TrimSpace(doc) == "" {
					return ports.ErrInvalid
				}
				if !slices.Contains(documents, doc) {
					documents = append(documents, doc)
				}
			}
		}
		sameResolution := resolution == q.Resolution || resolution != nil && q.Resolution != nil && *resolution == *q.Resolution
		sameDocuments := slices.Equal(documents, q.DocumentIDs)
		if from == in.State && sameResolution && sameDocuments {
			result = q
			return nil
		}
		if from != in.State && !(from == "new" && in.State != "new" || from == "in_progress" && in.State == "resolved" ||
			from == "resolved" && in.State == "in_progress") {
			return ErrTransition
		}
		if in.State == "resolved" && (resolution == nil || *resolution == "" || len(documents) == 0) {
			return ErrDocument
		}
		if (in.State == "resolved" || !sameDocuments) && len(documents) > 0 {
			if s.Documents == nil {
				return ports.ErrUnavailable
			}
			student := identity.Principal{UserID: q.Student.ID, TeamID: p.TeamID, Role: identity.Intern}
			if err = s.Documents.CheckReady(ctx, tx, p, student, documents); err != nil {
				if errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrForbidden) {
					return ErrDocument
				}
				return err
			}
		}
		snapshot, err := json.Marshal(q)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO knowledge_gap_history(question_id,revision,snapshot) VALUES ($1,$2,$3)`, id, q.Revision, string(snapshot)); err != nil {
			return err
		}
		links, err := json.Marshal(documents)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE knowledge_gaps SET status=$2,resolution=$3,document_ids=$4,mentor_id=$5,revision=revision+1,
    updated_at=clock_timestamp(),resolved_at=CASE WHEN $2='resolved' THEN clock_timestamp() ELSE resolved_at END WHERE id=$1`,
			id, in.State, resolution, string(links), p.UserID); err != nil {
			return err
		}
		result, err = scan(tx.QueryRowContext(ctx, `SELECT `+columns+joined+`WHERE q.id::text=$1`, id))
		return err
	})
	return result, mapped(err)
}
