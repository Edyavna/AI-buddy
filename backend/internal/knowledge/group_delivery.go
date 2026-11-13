package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func (s Uploads) GetGroup(ctx context.Context, id string) (QuestionGroup, error) {
	p, err := groupPrincipal(ctx)
	if err != nil {
		return QuestionGroup{}, err
	}
	var out QuestionGroup
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var e error
		out, e = lockGroup(ctx, tx, p, id)
		if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT question_id::text,status,error FROM question_group_members WHERE group_id=$1 AND removed_at IS NULL ORDER BY question_id`, id)
		if e != nil {
			return e
		}
		for rows.Next() {
			var m GroupMember
			if e = rows.Scan(&m.ID, &m.Status, &m.Error); e != nil {
				rows.Close()
				return e
			}
			out.Members = append(out.Members, m)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for i := range out.Members {
			m := &out.Members[i]
			q, e := groupQuestion(ctx, tx, p, m.ID)
			if errors.Is(e, sql.ErrNoRows) || errors.Is(e, ports.ErrNotFound) {
				m.Question = nil
				continue
			}
			if e != nil {
				return e
			}
			if q.TeamID != out.TeamID {
				continue
			}
			m.Question = &q
			if m.Status == "delivered" || m.Status == "closed" {
				out.DeliveredCount++
			}
		}
		rows, e = tx.QueryContext(ctx, `SELECT event,question_ids,created_at FROM question_group_history WHERE group_id=$1 ORDER BY id DESC LIMIT 100`, id)
		if e != nil {
			return e
		}
		for rows.Next() {
			var item GroupEvent
			var raw []byte
			if e = rows.Scan(&item.Event, &raw, &item.CreatedAt); e != nil {
				rows.Close()
				return e
			}
			if e = json.Unmarshal(raw, &item.QuestionIDs); e != nil {
				rows.Close()
				return e
			}
			out.History = append(out.History, item)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		groupCtx := withGroupDraft(ctx)
		out.Draft, e = readResolution(groupCtx, tx, id, p.UserID)
		if e != nil {
			return e
		}
		if out.Draft != nil && out.Draft.DocumentID != nil {
			q := ResolutionSource{ID: id, TeamID: out.TeamID, Direction: out.Direction}
			for _, m := range out.Members {
				if m.Question != nil {
					q.StudentID = m.Question.StudentID
					break
				}
			}
			if e = resolutionState(groupCtx, tx, p, q, out.Draft); e != nil {
				if !errors.Is(e, sql.ErrNoRows) && !errors.Is(e, ports.ErrNotFound) && !errors.Is(e, ports.ErrForbidden) {
					return e
				}
				message := "Опубликованная версия недоступна. Доставка остановлена. Можно архивировать состав и подготовить новую группу."
				out.Draft = nil
				out.PublicationError = &message
			}
		}
		if out.PublishedAt != nil {
			rows, e = tx.QueryContext(ctx, `SELECT q.question,q.step_id `+groupQuestionFrom+`WHERE `+groupQuestionAccess+` AND q.team_id=$4 AND q.created_at>$5 AND NOT EXISTS(SELECT 1 FROM question_group_members m WHERE m.question_id=q.id AND m.group_id=$6) ORDER BY q.created_at LIMIT 501`, p.Role, p.UserID, p.TeamID, out.TeamID, *out.PublishedAt, id)
			if e != nil {
				return e
			}
			count := 0
			for rows.Next() {
				var question string
				var step *string
				if e = rows.Scan(&question, &step); e != nil {
					rows.Close()
					return e
				}
				count++
				if count <= 500 {
					if _, reason := SimilarQuestions(out.Title, question, false); reason != "" {
						out.RepeatCount++
					}
				}
			}
			out.RepeatCountLimited = count > 500
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		return nil
	})
	return out, uploadError(err)
}

// Each recipient commits independently. A failed delivery cannot roll back
// successful links. A retry checks access and the latest revision again.
func (s Uploads) DeliverGroup(ctx context.Context, id string, revision int64, closeQuestions bool) (QuestionGroup, error) {
	p, err := groupPrincipal(ctx)
	if err != nil {
		return QuestionGroup{}, err
	}
	g, err := s.GetGroup(ctx, id)
	if err != nil {
		return g, err
	}
	if g.Revision != revision || g.Draft == nil || g.Draft.Status != "ready" || g.Draft.DocumentID == nil {
		return g, ports.ErrConflict
	}
	for _, member := range g.Members {
		err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
			current, e := lockGroup(ctx, tx, p, id)
			if e != nil {
				return e
			}
			if current.Revision != revision {
				return ports.ErrConflict
			}
			q, e := groupQuestion(ctx, tx, p, member.ID)
			if e != nil {
				return e
			}
			if q.TeamID != g.TeamID {
				return ports.ErrConflict
			}
			var status string
			var expected int64
			if e = tx.QueryRowContext(ctx, `SELECT status,question_revision FROM question_group_members WHERE group_id=$1 AND question_id=$2 AND removed_at IS NULL FOR UPDATE`, id, member.ID).Scan(&status, &expected); e != nil {
				return e
			}
			student := identity.Principal{UserID: q.StudentID, TeamID: q.TeamID, Role: identity.Intern}
			if e = s.CheckReady(ctx, tx, p, student, []string{*g.Draft.DocumentID}); e != nil {
				return e
			}
			// Delivery must cite the publication selected for this group, not a silently superseding version.
			var version int64
			if e = tx.QueryRowContext(ctx, `SELECT version FROM knowledge_documents WHERE id=$1 FOR SHARE`, *g.Draft.DocumentID).Scan(&version); e != nil {
				return e
			}
			if version != *g.Draft.DocumentVersion {
				return ports.ErrConflict
			}
			if status == "closed" || (!closeQuestions && status == "delivered") {
				return nil
			}
			if q.Status == "resolved" || q.Revision != expected {
				return ports.ErrConflict
			}
			if closeQuestions && status != "delivered" {
				return ports.ErrConflict
			}
			event := "group_instruction_delivered"
			next := "delivered"
			if closeQuestions {
				event = "group_question_closed"
				next = "closed"
			}
			snapshot, _ := json.Marshal(map[string]any{"event": event, "group_id": id, "document_id": *g.Draft.DocumentID, "document_version": version})
			if _, e = tx.ExecContext(ctx, `INSERT INTO knowledge_gap_history(question_id,revision,snapshot) VALUES($1,$2,$3)`, q.ID, q.Revision, string(snapshot)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE knowledge_gaps SET document_ids=CASE WHEN document_ids ? $2 THEN document_ids ELSE document_ids||jsonb_build_array($2::text) END,resolution=CASE WHEN $3 THEN $4 ELSE resolution END,status=CASE WHEN $3 THEN 'resolved' ELSE status END,resolved_at=CASE WHEN $3 THEN now() ELSE resolved_at END,revision=revision+1,updated_at=now() WHERE id=$1`, q.ID, *g.Draft.DocumentID, closeQuestions, g.Resolution); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE question_group_members SET status=$3,error=NULL,question_revision=$4 WHERE group_id=$1 AND question_id=$2`, id, q.ID, next, q.Revision+1); e != nil {
				return e
			}
			return groupEvent(ctx, tx, p, id, event, []string{q.ID})
		})
		if err != nil {
			// Fail closed without storing dependency errors or personal text. If the
			// database itself is unavailable, preserve prior successes and return 503.
			if e := s.recordGroupFailure(ctx, p, id, member.ID); e != nil {
				return QuestionGroup{}, uploadError(e)
			}
		}
	}
	return s.GetGroup(ctx, id)
}

func (s Uploads) recordGroupFailure(ctx context.Context, p identity.Principal, id, question string) error {
	return database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if _, e := lockGroup(ctx, tx, p, id); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE question_group_members SET status=CASE WHEN status IN ('delivered','closed') THEN status ELSE 'failed' END,error='Не удалось выполнить действие. Проверьте доступ, состояние и версию вопроса, затем повторите.' WHERE group_id=$1 AND question_id=$2 AND removed_at IS NULL`, id, question)
		return e
	})
}

type groupDraftKey struct{}

func withGroupDraft(ctx context.Context) context.Context {
	return context.WithValue(ctx, groupDraftKey{}, true)
}
func isGroupDraft(ctx context.Context) bool {
	flag, _ := ctx.Value(groupDraftKey{}).(bool)
	return flag
}
func resolutionStorage(ctx context.Context) (string, string) {
	if isGroupDraft(ctx) {
		return "resolution_group_drafts", "group_id"
	}
	return "resolution_drafts", "question_id"
}
func groupResolutionSource(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (ResolutionSource, error) {
	g, err := lockGroup(ctx, tx, p, id)
	if err != nil {
		return ResolutionSource{}, err
	}
	if strings.TrimSpace(g.Resolution) == "" {
		return ResolutionSource{}, ports.ErrConflict
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM question_group_members WHERE group_id=$1 AND removed_at IS NULL)`, id).Scan(&active); err != nil {
		return ResolutionSource{}, err
	}
	if !active {
		return ResolutionSource{}, ports.ErrConflict
	}
	return ResolutionSource{ID: g.ID, Direction: g.Direction, TeamID: g.TeamID, Question: g.Title, Resolution: g.Resolution, Revision: g.Revision}, nil
}
func checkGroupRecipients(ctx context.Context, tx *sql.Tx, p identity.Principal, id, team string) error {
	rows, err := tx.QueryContext(ctx, `SELECT question_id::text FROM question_group_members WHERE group_id=$1 AND removed_at IS NULL ORDER BY question_id`, id)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var qid string
		if err = rows.Scan(&qid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, qid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, qid := range ids {
		q, e := groupQuestion(ctx, tx, p, qid)
		if e != nil {
			return e
		}
		if q.Status == "resolved" || q.TeamID != team {
			return ports.ErrConflict
		}
	}
	return nil
}
