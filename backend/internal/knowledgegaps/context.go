package knowledgegaps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var ErrContextChanged = errors.New("context reference unavailable or changed")

type ContextFragmentInput struct {
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
}
type ContextDocumentInput struct {
	ID      string `json:"document_id"`
	Version int64  `json:"version"`
}
type ContextInput struct {
	Reviewed     bool                   `json:"reviewed"`
	IncludeStep  bool                   `json:"include_step"`
	StepRevision *int64                 `json:"step_revision"`
	Attempts     string                 `json:"attempts"`
	Fragments    []ContextFragmentInput `json:"fragments"`
	Documents    []ContextDocumentInput `json:"documents"`
}
type ContextFragment struct {
	MessageID string  `json:"message_id"`
	Role      string  `json:"role"`
	Text      *string `json:"text"`
	Edited    bool    `json:"edited"`
	Status    string  `json:"status"`
}
type ContextDocument struct {
	ID      string  `json:"document_id"`
	Version int64   `json:"version"`
	Title   *string `json:"title"`
	Opened  bool    `json:"opened"`
	Status  string  `json:"status"`
}
type ContextStep struct {
	ID       string  `json:"id"`
	Title    *string `json:"title"`
	Status   string  `json:"status"`
	Revision int64   `json:"revision"`
}
type ContextSnapshot struct {
	ConversationID string            `json:"conversation_id"`
	Attempts       string            `json:"attempts"`
	Step           *ContextStep      `json:"step"`
	Fragments      []ContextFragment `json:"fragments"`
	Documents      []ContextDocument `json:"documents"`
	Limited        bool              `json:"limited"`
}
type contextRoot struct {
	conversation string
	sequence     int64
	step         *string
}

func contextSource(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (contextRoot, error) {
	var root contextRoot
	err := tx.QueryRowContext(ctx, `SELECT c.id,a.sequence,c.step_id FROM chat_messages a JOIN conversations c ON c.id=a.conversation_id
 WHERE a.id=$1 AND a.role='assistant' AND a.status IN ('completed','failed','interrupted') AND c.user_id=$2 AND c.team_id=$3 FOR SHARE OF c,a`, id, p.UserID, p.TeamID).Scan(&root.conversation, &root.sequence, &root.step)
	return root, err
}
func clipContext(text string) string {
	return string([]rune(text)[:min(800, utf8.RuneCountInString(text))])
}

const contextMessages = ` FROM chat_messages m WHERE m.conversation_id=$1 AND m.sequence<=$2 AND m.status='completed'
 AND ((m.role='assistant' AND COALESCE(m.routing->>'direction','mentor') IN ($3,'needs_clarification'))
 OR (m.role='user' AND EXISTS(SELECT 1 FROM chat_messages a WHERE a.in_reply_to_message_id=m.id AND a.conversation_id=m.conversation_id
 AND a.sequence<=$2 AND COALESCE(a.routing->>'direction','mentor') IN ($3,'needs_clarification'))))`

func fragmentSource(ctx context.Context, tx *sql.Tx, root contextRoot, student, viewer identity.Principal, direction, id string) (string, string, error) {
	var role, text string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT m.role,m.text,m.sources`+contextMessages+` AND m.id=$4 FOR SHARE OF m`, root.conversation, root.sequence, direction, id).Scan(&role, &text, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrContextChanged
	}
	if err != nil {
		return "", "", err
	}
	if role == "assistant" {
		var sources []struct {
			ID      string `json:"document_id"`
			Version int64  `json:"version"`
		}
		if err = json.Unmarshal(raw, &sources); err != nil {
			return "", "", err
		}
		for _, src := range sources {
			doc, e := knowledge.ReadContextDocumentTx(ctx, tx, student, viewer, src.ID, direction)
			if errors.Is(e, sql.ErrNoRows) || e == nil && src.Version > 0 && doc.Version != src.Version {
				return "", "", ErrContextChanged
			}
			if e != nil {
				return "", "", e
			}
		}
	}
	return role, text, nil
}

func contextStep(ctx context.Context, tx *sql.Tx, p identity.Principal, id *string) (*ContextStep, error) {
	if id == nil {
		return nil, nil
	}
	out := &ContextStep{ID: *id, Status: "available"}
	var title string
	err := tx.QueryRowContext(ctx, `SELECT s.title,p.revision FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id WHERE s.id=$1 AND p.user_id=$2 AND p.team_id=$3 FOR SHARE OF p,s`, *id, p.UserID, p.TeamID).Scan(&title, &out.Revision)
	if err == nil {
		out.Title = &title
	}
	return out, err
}

func validContext(in *ContextInput) bool {
	if in == nil {
		return true
	}
	if in.Fragments == nil || in.Documents == nil || in.IncludeStep && (in.StepRevision == nil || *in.StepRevision < 1) {
		return false
	}
	if utf8.RuneCountInString(in.Attempts) > 4000 || strings.ContainsRune(in.Attempts, 0) || len(in.Fragments) > 12 || len(in.Documents) > 10 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range in.Fragments {
		if f.MessageID == "" || seen[f.MessageID] || utf8.RuneCountInString(f.Text) > 800 || strings.ContainsRune(f.Text, 0) {
			return false
		}
		seen[f.MessageID] = true
	}
	seen = map[string]bool{}
	for _, d := range in.Documents {
		if d.ID == "" || seen[d.ID] || d.Version < 1 {
			return false
		}
		seen[d.ID] = true
	}
	return true
}

func offeredDocument(ctx context.Context, tx *sql.Tx, root contextRoot, id string) error {
	var found bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM chat_messages m CROSS JOIN LATERAL jsonb_array_elements(m.sources) s
 WHERE m.conversation_id=$1 AND m.sequence<=$2 AND s->>'document_id'=$3)`, root.conversation, root.sequence, id).Scan(&found)
	if err == nil && !found {
		return sql.ErrNoRows
	}
	return err
}

func openedDocument(ctx context.Context, tx *sql.Tx, student string, root contextRoot, id string, version int64) (bool, error) {
	var opened bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM question_document_opens o JOIN chat_messages m ON m.id=o.message_id
 WHERE o.student_id=$1 AND m.conversation_id=$2 AND m.sequence<=$3 AND o.document_id=$4 AND o.version=$5)`, student, root.conversation, root.sequence, id, version).Scan(&opened)
	return opened, err
}

// Generates a bounded extractive preview. It never invents attempts or facts
// and does not send chat history to a model. Nothing here is pre-approved.
func (s Store) ContextPreview(ctx context.Context, id, direction string) (ContextSnapshot, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return ContextSnapshot{}, err
	}
	out := ContextSnapshot{Fragments: []ContextFragment{}, Documents: []ContextDocument{}}
	if direction != "mentor" && direction != "hr" {
		return out, ports.ErrInvalid
	}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := hrscope.LockTeam(ctx, tx, p.TeamID); err != nil {
			return err
		}
		root, e := contextSource(ctx, tx, p, id)
		if e != nil {
			return e
		}
		out.ConversationID = root.conversation
		out.Step, e = contextStep(ctx, tx, p, root.step)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e != nil {
			out.Step.Status = "unavailable"
		}
		rows, e := tx.QueryContext(ctx, `SELECT m.id,m.role,m.text`+contextMessages+` ORDER BY m.sequence DESC LIMIT 13`, root.conversation, root.sequence, direction)
		if e != nil {
			return e
		}
		for rows.Next() {
			var f ContextFragment
			var text string
			if e = rows.Scan(&f.MessageID, &f.Role, &text); e != nil {
				rows.Close()
				return e
			}
			text = clipContext(text)
			f.Text = &text
			f.Status = "available"
			out.Fragments = append(out.Fragments, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(out.Fragments) > 12 {
			out.Limited = true
			out.Fragments = out.Fragments[:12]
		}
		allowed := []ContextFragment{}
		for _, f := range out.Fragments {
			_, _, e := fragmentSource(ctx, tx, root, p, p, direction, f.MessageID)
			if errors.Is(e, ErrContextChanged) {
				continue
			}
			if e != nil {
				return e
			}
			allowed = append(allowed, f)
		}
		out.Fragments = allowed
		rows, e = tx.QueryContext(ctx, `SELECT DISTINCT src->>'document_id' FROM chat_messages m CROSS JOIN LATERAL jsonb_array_elements(m.sources) src WHERE m.conversation_id=$1 AND m.sequence<=$2 ORDER BY 1 LIMIT 11`, root.conversation, root.sequence)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var doc string
			if e = rows.Scan(&doc); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, doc)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(ids) > 10 {
			out.Limited = true
			ids = ids[:10]
		}
		for _, doc := range ids {
			d, e := knowledge.ReadContextDocumentTx(ctx, tx, p, p, doc, direction)
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			if e != nil {
				return e
			}
			opened, e := openedDocument(ctx, tx, p.UserID, root, doc, d.Version)
			if e != nil {
				return e
			}
			out.Documents = append(out.Documents, ContextDocument{ID: d.ID, Version: d.Version, Title: &d.Title, Opened: opened, Status: "available"})
		}
		return nil
	})
	return out, mapped(err)
}

func buildContext(ctx context.Context, tx *sql.Tx, student, viewer identity.Principal, messageID, direction string, in *ContextInput) (*ContextSnapshot, error) {
	if in == nil {
		return nil, nil
	}
	root, err := contextSource(ctx, tx, student, messageID)
	if err != nil {
		return nil, err
	}
	out := &ContextSnapshot{ConversationID: root.conversation, Attempts: in.Attempts, Fragments: []ContextFragment{}, Documents: []ContextDocument{}}
	if in.IncludeStep {
		out.Step, err = contextStep(ctx, tx, student, root.step)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err != nil || out.Step == nil || in.StepRevision == nil || out.Step.Revision != *in.StepRevision {
			return nil, ErrContextChanged
		}
	}
	for _, f := range in.Fragments {
		role, text, err := fragmentSource(ctx, tx, root, student, viewer, direction, f.MessageID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrContextChanged
			}
			return nil, err
		}
		value := f.Text
		out.Fragments = append(out.Fragments, ContextFragment{MessageID: f.MessageID, Role: role, Text: &value, Edited: value != clipContext(text), Status: "available"})
	}
	for _, item := range in.Documents {
		if err = offeredDocument(ctx, tx, root, item.ID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrContextChanged
			}
			return nil, err
		}
		d, e := knowledge.ReadContextDocumentTx(ctx, tx, student, viewer, item.ID, direction)
		if errors.Is(e, sql.ErrNoRows) || e == nil && d.Version != item.Version {
			return nil, ErrContextChanged
		}
		if e != nil {
			return nil, e
		}
		opened, e := openedDocument(ctx, tx, student.UserID, root, item.ID, item.Version)
		if e != nil {
			return nil, e
		}
		out.Documents = append(out.Documents, ContextDocument{ID: d.ID, Version: d.Version, Title: &d.Title, Opened: opened, Status: "available"})
	}
	return out, nil
}

// Reading alone does not claim the document was shown. The drawer separately
// acknowledges its successful render, with another ACL/version check.
func (s Store) OpenContextDocument(ctx context.Context, messageID, id string) (ports.Document, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return ports.Document{}, err
	}
	var out ports.Document
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var e error
		out, e = citedContextDocument(ctx, tx, p, messageID, id)
		return e
	})
	return out, mapped(err)
}

func citedContextDocument(ctx context.Context, tx *sql.Tx, p identity.Principal, messageID, id string) (ports.Document, error) {
	if e := hrscope.LockTeam(ctx, tx, p.TeamID); e != nil {
		return ports.Document{}, e
	}
	root, e := contextSource(ctx, tx, p, messageID)
	if e != nil {
		return ports.Document{}, e
	}
	if e = offeredDocument(ctx, tx, root, id); e != nil {
		return ports.Document{}, e
	}
	var direction string
	if e = tx.QueryRowContext(ctx, `SELECT direction FROM knowledge_documents WHERE id=$1`, id).Scan(&direction); e != nil {
		return ports.Document{}, e
	}
	return knowledge.ReadContextDocumentTx(ctx, tx, p, p, id, direction)
}
func (s Store) RecordContextOpen(ctx context.Context, messageID, id string, version int64) error {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return err
	}
	if version < 1 {
		return ports.ErrInvalid
	}
	return mapped(database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		doc, e := citedContextDocument(ctx, tx, p, messageID, id)
		if e != nil {
			return e
		}
		if doc.Version != version {
			return ErrContextChanged
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO question_document_opens(student_id,message_id,document_id,version) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, p.UserID, messageID, id, version)
		return e
	}))
}
