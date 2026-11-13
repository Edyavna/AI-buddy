package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type GroupQuestion struct {
	ID          string    `json:"id"`
	TeamID      string    `json:"team_id"`
	Question    string    `json:"question"`
	StudentID   string    `json:"student_id"`
	StudentName string    `json:"student_name"`
	StepID      *string   `json:"step_id"`
	Revision    int64     `json:"revision"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}
type GroupMember struct {
	ID       string         `json:"id"`
	Question *GroupQuestion `json:"question"`
	Status   string         `json:"status"`
	Error    *string        `json:"error"`
}
type GroupEvent struct {
	Event       string    `json:"event"`
	QuestionIDs []string  `json:"question_ids"`
	CreatedAt   time.Time `json:"created_at"`
}
type QuestionGroup struct {
	ID                 string           `json:"id"`
	AuthorID           string           `json:"author_id"`
	Direction          string           `json:"direction"`
	TeamID             string           `json:"team_id"`
	Title              string           `json:"title"`
	Resolution         string           `json:"resolution"`
	Revision           int64            `json:"revision"`
	PublishedAt        *time.Time       `json:"published_at"`
	Members            []GroupMember    `json:"members"`
	History            []GroupEvent     `json:"history"`
	Draft              *ResolutionDraft `json:"draft"`
	PublicationError   *string          `json:"publication_error,omitempty"`
	DeliveredCount     int              `json:"delivered_count"`
	RepeatCount        int              `json:"repeat_count"`
	RepeatCountLimited bool             `json:"repeat_count_limited"`
}
type GroupInput struct {
	ID          string   `json:"operation_id"`
	Revision    int64    `json:"revision"`
	Title       string   `json:"title"`
	Resolution  string   `json:"resolution"`
	QuestionIDs []string `json:"question_ids"`
}
type GroupSuggestion struct {
	QuestionIDs []string `json:"question_ids"`
	Reason      string   `json:"reason"`
	Score       float64  `json:"score"`
}
type GroupWorkspace struct {
	Groups      []QuestionGroup   `json:"groups"`
	Candidates  []GroupQuestion   `json:"candidates"`
	Suggestions []GroupSuggestion `json:"suggestions"`
	Limited     bool              `json:"limited"`
}

const groupQuestionColumns = `q.id::text,q.team_id,q.question,q.student_id,u.name,q.step_id,q.revision,q.status,q.created_at`
const groupQuestionFrom = ` FROM knowledge_gaps q JOIN users u ON u.id=q.student_id AND u.team_id=q.team_id `
const groupQuestionAccess = `q.direction=$1 AND (($1='mentor' AND q.team_id=$3 AND EXISTS(SELECT 1 FROM mentor_assignments a WHERE a.intern_id=q.student_id AND a.team_id=q.team_id AND a.mentor_id=$2)) OR ($1='hr' AND q.hr_id=$2 AND EXISTS(SELECT 1 FROM hr_team_assignments a WHERE a.hr_user_id=$2 AND a.team_id=q.team_id AND a.active)))`
const groupColumns = `id::text,author_id,direction,team_id,title,resolution,revision,published_at`

type groupScan interface{ Scan(...any) error }

func scanGroupQuestion(row groupScan) (GroupQuestion, error) {
	var q GroupQuestion
	err := row.Scan(&q.ID, &q.TeamID, &q.Question, &q.StudentID, &q.StudentName, &q.StepID, &q.Revision, &q.Status, &q.CreatedAt)
	return q, err
}
func scanGroup(row groupScan) (QuestionGroup, error) {
	g := QuestionGroup{Members: []GroupMember{}, History: []GroupEvent{}}
	err := row.Scan(&g.ID, &g.AuthorID, &g.Direction, &g.TeamID, &g.Title, &g.Resolution, &g.Revision, &g.PublishedAt)
	return g, err
}
func groupPrincipal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err == nil && p.Role != identity.Mentor && p.Role != identity.HR {
		err = ports.ErrForbidden
	}
	return p, err
}
func groupQuestion(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (GroupQuestion, error) {
	q, err := scanGroupQuestion(tx.QueryRowContext(ctx, `SELECT `+groupQuestionColumns+groupQuestionFrom+`WHERE `+groupQuestionAccess+` AND q.id::text=$4 FOR UPDATE OF q`, p.Role, p.UserID, p.TeamID, id))
	if err != nil {
		return q, err
	}
	if p.Role == identity.HR {
		return q, hrscope.CheckTeam(ctx, tx, p.UserID, q.TeamID)
	}
	var found int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM mentor_assignments WHERE intern_id=$1 AND team_id=$2 AND mentor_id=$3 FOR SHARE`, q.StudentID, q.TeamID, p.UserID).Scan(&found)
	return q, err
}
func lockGroup(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (QuestionGroup, error) {
	g, err := scanGroup(tx.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM question_groups WHERE id::text=$1 AND author_id=$2 AND direction=$3 FOR UPDATE`, id, p.UserID, p.Role))
	if err != nil {
		return g, err
	}
	if p.Role == identity.HR {
		if err = hrscope.CheckTeam(ctx, tx, p.UserID, g.TeamID); err != nil {
			return g, err
		}
	} else if p.TeamID != g.TeamID {
		return g, ports.ErrNotFound
	}
	var visible bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM question_group_members m JOIN knowledge_gaps q ON q.id=m.question_id WHERE m.group_id=$4 AND q.team_id=$5 AND `+groupQuestionAccess+`)`, p.Role, p.UserID, p.TeamID, id, g.TeamID).Scan(&visible)
	if err == nil && !visible {
		err = ports.ErrNotFound
	}
	return g, err
}
func groupEvent(ctx context.Context, tx *sql.Tx, p identity.Principal, id, event string, ids []string) error {
	raw, _ := json.Marshal(ids)
	_, err := tx.ExecContext(ctx, `INSERT INTO question_group_history(group_id,author_id,event,question_ids) VALUES($1,$2,$3,$4)`, id, p.UserID, event, string(raw))
	return err
}
func (s Uploads) GroupWorkspace(ctx context.Context) (GroupWorkspace, error) {
	out := GroupWorkspace{Groups: []QuestionGroup{}, Candidates: []GroupQuestion{}, Suggestions: []GroupSuggestion{}}
	p, err := groupPrincipal(ctx)
	if err != nil {
		return out, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+groupQuestionColumns+groupQuestionFrom+`WHERE `+groupQuestionAccess+` AND q.status<>'resolved' AND NOT EXISTS(SELECT 1 FROM question_group_members m JOIN question_groups g ON g.id=m.group_id WHERE m.question_id=q.id AND m.removed_at IS NULL AND g.author_id=$2 AND g.direction=$1) ORDER BY q.created_at DESC,q.id LIMIT 101`, p.Role, p.UserID, p.TeamID)
	if err != nil {
		return out, uploadError(err)
	}
	for rows.Next() {
		q, e := scanGroupQuestion(rows)
		if e != nil {
			rows.Close()
			return out, uploadError(e)
		}
		out.Candidates = append(out.Candidates, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, uploadError(err)
	}
	if len(out.Candidates) > 100 {
		out.Candidates = out.Candidates[:100]
		out.Limited = true
	}
	for i, a := range out.Candidates {
		for _, b := range out.Candidates[i+1:] {
			if a.TeamID != b.TeamID {
				continue
			}
			same := a.StepID != nil && b.StepID != nil && *a.StepID == *b.StepID
			if score, reason := SimilarQuestions(a.Question, b.Question, same); reason != "" && len(out.Suggestions) < 30 {
				out.Suggestions = append(out.Suggestions, GroupSuggestion{[]string{a.ID, b.ID}, reason, score})
			}
		}
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT id::text FROM question_groups WHERE author_id=$1 AND direction=$2 ORDER BY updated_at DESC,id LIMIT 100`, p.UserID, p.Role)
	if err != nil {
		return out, uploadError(err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, uploadError(err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, uploadError(err)
	}
	for _, id := range ids {
		g, e := s.GetGroup(ctx, id)
		if errors.Is(e, ports.ErrNotFound) {
			continue
		}
		if e != nil {
			return out, e
		}
		out.Groups = append(out.Groups, g)
	}
	return out, nil
}

func (s Uploads) SaveGroup(ctx context.Context, id string, in GroupInput) (QuestionGroup, error) {
	p, err := groupPrincipal(ctx)
	if err != nil {
		return QuestionGroup{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Resolution = strings.TrimSpace(in.Resolution)
	slices.Sort(in.QuestionIDs)
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 || utf8.RuneCountInString(in.Resolution) > 8000 || !validText(in.Title+in.Resolution) || (len(in.QuestionIDs) == 0 && in.Revision == 0) || len(in.QuestionIDs) > 30 || len(slices.Compact(slices.Clone(in.QuestionIDs))) != len(in.QuestionIDs) || in.Revision < 0 {
		return QuestionGroup{}, ports.ErrInvalid
	}
	if id == "" {
		id = in.ID
	}
	if !uuidPattern.MatchString(id) {
		return QuestionGroup{}, ports.ErrInvalid
	}
	for _, q := range in.QuestionIDs {
		if !uuidPattern.MatchString(q) {
			return QuestionGroup{}, ports.ErrInvalid
		}
	}
	raw, _ := json.Marshal(in)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(58018,hashtext($1))`, id); e != nil {
			return e
		}
		var g QuestionGroup
		// Creation is idempotent by the client UUID. An existing foreign object is never disclosed.
		var oldHash, owner, direction string
		e := tx.QueryRowContext(ctx, `SELECT create_hash,author_id,direction FROM question_groups WHERE id=$1`, id).Scan(&oldHash, &owner, &direction)
		if e == nil && (owner != p.UserID || direction != string(p.Role)) {
			return ports.ErrNotFound
		}
		created := errors.Is(e, sql.ErrNoRows)
		if e != nil && !created {
			return e
		}
		if !created {
			g, e = lockGroup(ctx, tx, p, id)
			if e != nil {
				return e
			}
			if in.Revision == 0 {
				if hash == oldHash {
					return nil
				}
				return ports.ErrConflict
			}
			if in.Revision != g.Revision {
				return ports.ErrConflict
			}
		}
		if created && in.Revision != 0 {
			return ports.ErrNotFound
		}
		if !created {
			var publishing bool
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resolution_group_drafts WHERE group_id=$1 AND document_id IS NOT NULL)`, id).Scan(&publishing); e != nil {
				return e
			}
			if publishing && (in.Title != g.Title || in.Resolution != g.Resolution) {
				return ports.ErrConflict
			}
			if publishing && g.PublishedAt == nil && len(in.QuestionIDs) > 0 {
				return ports.ErrConflict
			}
			if publishing {
				for _, qid := range in.QuestionIDs {
					var existed bool
					if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM question_group_members WHERE group_id=$1 AND question_id=$2 AND removed_at IS NULL)`, id, qid).Scan(&existed); e != nil {
						return e
					}
					if !existed {
						return ports.ErrConflict
					}
				}
			}
		}
		questions := []GroupQuestion{}
		team := ""
		for _, qid := range in.QuestionIDs {
			q, e := groupQuestion(ctx, tx, p, qid)
			if e != nil {
				return e
			}
			if q.Status == "resolved" {
				var closed bool
				if created {
					return ports.ErrConflict
				}
				if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM question_group_members WHERE group_id=$1 AND question_id=$2 AND status='closed' AND removed_at IS NULL)`, id, qid).Scan(&closed); e != nil {
					return e
				}
				if !closed {
					return ports.ErrConflict
				}
			}
			if team != "" && team != q.TeamID {
				return ports.ErrConflict
			}
			team = q.TeamID
			questions = append(questions, q)
		}
		if !created && len(questions) > 0 && team != g.TeamID {
			return ports.ErrConflict
		}
		if created {
			if _, e = tx.ExecContext(ctx, `INSERT INTO question_groups(id,author_id,direction,team_id,title,resolution,create_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, p.UserID, p.Role, team, in.Title, in.Resolution, hash); e != nil {
				return e
			}
		} else {
			if _, e = tx.ExecContext(ctx, `UPDATE question_groups SET title=$2,resolution=$3,revision=revision+1,updated_at=now() WHERE id=$1`, id, in.Title, in.Resolution); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE question_group_members SET removed_at=now() WHERE group_id=$1 AND removed_at IS NULL`, id); e != nil {
			return e
		}
		for _, q := range questions {
			// Current question/assignment locks establish ownership. Release only a
			// previous specialist's membership; never merge another group's answer.
			if _, e = tx.ExecContext(ctx, `WITH released AS (
				UPDATE question_group_members m SET removed_at=now() FROM question_groups g
				WHERE m.group_id=g.id AND m.question_id=$1 AND m.removed_at IS NULL
				AND (g.author_id<>$2 OR g.direction<>$3) RETURNING m.group_id
			) INSERT INTO question_group_history(group_id,author_id,event,question_ids)
			SELECT group_id,$2,'owner_changed',jsonb_build_array($1::text) FROM released`, q.ID, p.UserID, p.Role); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO question_group_members(group_id,question_id,question_revision) VALUES($1,$2,$3) ON CONFLICT(group_id,question_id) DO UPDATE SET question_revision=EXCLUDED.question_revision,removed_at=NULL,status=CASE WHEN question_group_members.status='failed' THEN 'pending' ELSE question_group_members.status END,error=NULL`, id, q.ID, q.Revision); e != nil {
				var state interface{ SQLState() string }
				if errors.As(e, &state) && state.SQLState() == "23505" {
					return ports.ErrConflict
				}
				return e
			}
		}
		return groupEvent(ctx, tx, p, id, "composition_confirmed", in.QuestionIDs)
	})
	if err != nil {
		return QuestionGroup{}, uploadError(err)
	}
	return s.GetGroup(ctx, id)
}
