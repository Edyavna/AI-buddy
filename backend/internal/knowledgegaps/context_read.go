package knowledgegaps

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func contextQuestion(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (Question, error) {
	where := ownQuestion
	args := []any{id, p.UserID, p.TeamID}
	if p.Role == identity.Mentor {
		where = "WHERE " + assigned + " AND q.id::text=$3"
		args = []any{p.UserID, p.TeamID, id}
	}
	if p.Role == identity.HR {
		where = "WHERE " + hrQuestion + " AND q.id::text=$2"
		args = []any{p.UserID, id}
	}
	var team string
	if err := tx.QueryRowContext(ctx, "SELECT q.team_id"+joined+where, args...).Scan(&team); err != nil {
		return Question{}, err
	}
	if err := hrscope.LockTeam(ctx, tx, team); err != nil {
		return Question{}, err
	}
	q, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+joined+where+" FOR SHARE OF q", args...))
	if err != nil {
		return q, err
	}
	if p.Role == identity.HR {
		err = hrscope.CheckTeam(ctx, tx, p.UserID, q.TeamID)
	}
	if p.Role == identity.Mentor {
		var found int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM mentor_assignments WHERE mentor_id=$1 AND intern_id=$2 AND team_id=$3 FOR SHARE`, p.UserID, q.Student.ID, q.TeamID).Scan(&found)
	}
	return q, err
}

func (s Store) filterContext(ctx context.Context, p identity.Principal, q *Question) error {
	if q.ContextSnapshot == nil {
		return nil
	}
	return database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		current, err := contextQuestion(ctx, tx, p, q.ID)
		if err != nil {
			return err
		}
		if current.Revision != q.Revision {
			return ErrChanged
		}
		student := identity.Principal{UserID: current.Student.ID, TeamID: current.TeamID, Role: identity.Intern}
		snap := current.ContextSnapshot
		if snap == nil {
			q.ContextSnapshot = nil
			return nil
		}
		root, err := contextSource(ctx, tx, student, current.MessageID)
		if err != nil {
			return err
		}
		if snap.Step != nil {
			step, e := contextStep(ctx, tx, student, root.step)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e != nil || step == nil || step.ID != snap.Step.ID {
				snap.Step.Title = nil
				snap.Step.Status = "unavailable"
			}
		}
		for i := range snap.Fragments {
			f := &snap.Fragments[i]
			_, _, e := fragmentSource(ctx, tx, root, student, p, current.Direction, f.MessageID)
			if e != nil && !errors.Is(e, ErrContextChanged) {
				return e
			}
			if e != nil {
				f.Text = nil
				f.Status = "unavailable"
			}
		}
		for i := range snap.Documents {
			item := &snap.Documents[i]
			doc, e := knowledge.ReadContextDocumentTx(ctx, tx, student, p, item.ID, current.Direction)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e != nil || doc.Version != item.Version {
				item.Title = nil
				item.Status = "unavailable"
			}
		}
		q.ContextSnapshot = snap
		return nil
	})
}

func (s Store) ContextDocument(ctx context.Context, id, docID string) (ports.Document, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return ports.Document{}, err
	}
	var out ports.Document
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		q, e := contextQuestion(ctx, tx, p, id)
		if e != nil {
			return e
		}
		if q.ContextSnapshot == nil {
			return sql.ErrNoRows
		}
		var version int64
		for _, d := range q.ContextSnapshot.Documents {
			if d.ID == docID {
				version = d.Version
			}
		}
		if version == 0 {
			return sql.ErrNoRows
		}
		student := identity.Principal{UserID: q.Student.ID, TeamID: q.TeamID, Role: identity.Intern}
		out, e = knowledge.ReadContextDocumentTx(ctx, tx, student, p, docID, q.Direction)
		if e == nil && out.Version != version {
			return sql.ErrNoRows
		}
		return e
	})
	return out, mapped(err)
}

func sanitizeContextDraft(ctx context.Context, tx *sql.Tx, p identity.Principal, id string, d *Draft) error {
	if d.ContextSnapshot == nil {
		return nil
	}
	root, err := contextSource(ctx, tx, p, id)
	if err != nil {
		return err
	}
	for i := range d.ContextSnapshot.Fragments {
		f := &d.ContextSnapshot.Fragments[i]
		_, _, err = fragmentSource(ctx, tx, root, p, p, d.Direction, f.MessageID)
		if err != nil && !errors.Is(err, ErrContextChanged) {
			return err
		}
		if err != nil {
			f.Text = ""
		}
	}
	return nil
}
