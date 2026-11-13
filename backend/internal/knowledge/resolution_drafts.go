package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type ResolutionSource struct {
	ID         string  `json:"id"`
	Direction  string  `json:"direction"`
	TeamID     string  `json:"team_id"`
	StudentID  string  `json:"-"`
	Question   string  `json:"question"`
	Resolution string  `json:"resolution"`
	StepID     *string `json:"step_id"`
	Revision   int64   `json:"revision"`
}
type ResolutionTarget struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Version int64    `json:"version"`
	Content string   `json:"content"`
	Teams   []string `json:"team_ids"`
}
type ResolutionInput struct {
	Revision    int64   `json:"revision"`
	Title       string  `json:"title"`
	Content     string  `json:"content"`
	TargetID    *string `json:"target_document_id"`
	BaseVersion *int64  `json:"base_version"`
}
type ResolutionDraft struct {
	ResolutionInput
	TeamIDs         []string         `json:"team_ids"`
	Source          ResolutionSource `json:"source"`
	BaseContent     string           `json:"base_content"`
	DocumentID      *string          `json:"document_id"`
	DocumentVersion *int64           `json:"document_version"`
	Status          string           `json:"status"`
	Error           *string          `json:"error"`
}
type ResolutionWorkspace struct {
	Source  ResolutionSource   `json:"source"`
	Draft   *ResolutionDraft   `json:"draft"`
	Targets []ResolutionTarget `json:"targets"`
}

// Every read and write rechecks the current assignment, direction and source.
// The same locks are held through publication and the final indexing commit.
func resolutionSource(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (ResolutionSource, error) {
	var q ResolutionSource
	if p.Role != identity.Mentor && p.Role != identity.HR {
		return q, ports.ErrForbidden
	}
	err := tx.QueryRowContext(ctx, `SELECT q.id::text,q.direction,q.team_id,q.student_id,q.question,q.resolution,q.step_id,q.revision
 FROM knowledge_gaps q WHERE q.id::text=$1 AND q.status='resolved' AND NULLIF(trim(q.resolution),'') IS NOT NULL
 AND (($2='mentor' AND q.direction='mentor' AND q.team_id=$4 AND EXISTS(SELECT 1 FROM mentor_assignments a WHERE a.intern_id=q.student_id AND a.team_id=q.team_id AND a.mentor_id=$3))
 OR ($2='hr' AND q.direction='hr' AND q.hr_id=$3)) FOR SHARE OF q`, id, p.Role, p.UserID, p.TeamID).Scan(&q.ID, &q.Direction, &q.TeamID, &q.StudentID, &q.Question, &q.Resolution, &q.StepID, &q.Revision)
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

func resolutionTarget(ctx context.Context, tx *sql.Tx, p identity.Principal, q ResolutionSource, id string) (ResolutionTarget, error) {
	var target ResolutionTarget
	if !uuidPattern.MatchString(id) {
		return target, ports.ErrNotFound
	}
	if p.Role == identity.HR {
		d, err := lockEditor(ctx, tx, p.UserID, id)
		if err != nil {
			return target, err
		}
		if d.Revoked || d.Status != "ready" || d.Published == nil || *d.Published != d.Version {
			return target, ports.ErrConflict
		}
		found := false
		for _, team := range d.Teams {
			if team == q.TeamID {
				found = true
			}
		}
		if !found {
			return target, ports.ErrNotFound
		}
		return ResolutionTarget{d.ID, d.Title, d.Version, d.Content, d.Teams}, nil
	}
	err := tx.QueryRowContext(ctx, `SELECT u.id::text,u.title,u.version,u.content FROM mentor_documents u
 JOIN knowledge_documents d ON d.id=u.id::text WHERE u.id=$1 AND u.author_id=$2 AND u.team_id=$3 AND u.status='ready'
 AND d.team_id=u.team_id AND d.direction='mentor' AND d.users='[]'::jsonb FOR UPDATE OF u FOR SHARE OF d`, id, p.UserID, q.TeamID).Scan(&target.ID, &target.Title, &target.Version, &target.Content)
	target.Teams = []string{q.TeamID}
	return target, err
}

func readResolution(ctx context.Context, tx *sql.Tx, id, user string) (*ResolutionDraft, error) {
	var raw []byte
	var revision int64
	var doc *string
	var version *int64
	err := tx.QueryRowContext(ctx, `SELECT revision,payload,document_id::text,document_version FROM resolution_drafts WHERE question_id::text=$1 AND author_id=$2 FOR UPDATE`, id, user).Scan(&revision, &raw, &doc, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d ResolutionDraft
	if err = json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	d.Revision, d.DocumentID, d.DocumentVersion = revision, doc, version
	return &d, nil
}

func (s Uploads) ResolutionWorkspace(ctx context.Context, id string) (ResolutionWorkspace, error) {
	var out ResolutionWorkspace
	p, err := identity.Require(ctx)
	if err != nil {
		return out, err
	}
	out.Targets = []ResolutionTarget{}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var e error
		out.Source, e = resolutionSource(ctx, tx, p, id)
		if e != nil {
			return e
		}
		out.Draft, e = readResolution(ctx, tx, id, p.UserID)
		if e != nil {
			return e
		}
		if out.Draft != nil {
			if out.Draft.TargetID != nil && out.Draft.DocumentID == nil {
				if _, e = resolutionTarget(ctx, tx, p, out.Source, *out.Draft.TargetID); e != nil {
					return e
				}
			}
			if out.Draft.DocumentID != nil {
				if e = resolutionState(ctx, tx, p, out.Source, out.Draft); e != nil {
					return e
				}
			}
		}
		query := `SELECT d.id::text,d.title,d.version FROM mentor_documents d JOIN knowledge_documents k ON k.id=d.id::text WHERE d.author_id=$1 AND d.team_id=$2 AND d.status='ready' AND k.users='[]'::jsonb ORDER BY d.updated_at DESC LIMIT 100`
		args := []any{p.UserID, out.Source.TeamID}
		if p.Role == identity.HR {
			query = `SELECT d.id::text,v.title,v.version FROM hr_documents d JOIN hr_document_versions v ON v.document_id=d.id AND v.version=d.latest_version WHERE ` + hrEditor + ` AND NOT d.revoked AND v.status='ready' AND d.published_version=v.version AND EXISTS(SELECT 1 FROM hr_document_teams t WHERE t.document_id=d.id AND t.version=v.version AND t.team_id=$2) ORDER BY v.updated_at DESC LIMIT 100`
		}
		rows, e := tx.QueryContext(ctx, query, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var t ResolutionTarget
			t.Teams = []string{out.Source.TeamID}
			if e = rows.Scan(&t.ID, &t.Title, &t.Version); e != nil {
				return e
			}
			out.Targets = append(out.Targets, t)
		}
		return rows.Err()
	})
	return out, uploadError(err)
}

func (s Uploads) ResolutionTarget(ctx context.Context, id, target string) (ResolutionTarget, error) {
	var out ResolutionTarget
	p, err := identity.Require(ctx)
	if err != nil {
		return out, err
	}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		q, e := resolutionSource(ctx, tx, p, id)
		if e != nil {
			return e
		}
		out, e = resolutionTarget(ctx, tx, p, q, target)
		return e
	})
	return out, uploadError(err)
}

func (s Uploads) SaveResolution(ctx context.Context, id string, in ResolutionInput) (ResolutionDraft, error) {
	var out ResolutionDraft
	p, err := identity.Require(ctx)
	if err != nil {
		return out, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Revision < 0 || in.Title == "" || utf8.RuneCountInString(in.Title) > 200 || !validText(in.Title+in.Content) || len(in.Content) > MaxMarkdownBytes || (in.TargetID == nil) != (in.BaseVersion == nil) || (in.BaseVersion != nil && *in.BaseVersion < 1) {
		return out, ports.ErrInvalid
	}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		// Serializes the first save as well as later saves for this author/source.
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(56016,hashtext($1 || ':' || $2))`, id, p.UserID); e != nil {
			return e
		}
		q, e := resolutionSource(ctx, tx, p, id)
		if e != nil {
			return e
		}
		old, e := readResolution(ctx, tx, id, p.UserID)
		if e != nil {
			return e
		}
		revision := int64(0)
		if old != nil {
			revision = old.Revision
			if old.DocumentID != nil {
				return ports.ErrConflict
			}
		}
		if revision != in.Revision {
			return ports.ErrConflict
		}
		out = ResolutionDraft{ResolutionInput: in, Source: q, Status: "draft", TeamIDs: []string{q.TeamID}}
		if in.TargetID != nil {
			target, e := resolutionTarget(ctx, tx, p, q, *in.TargetID)
			if e != nil {
				return e
			}
			if target.Version != *in.BaseVersion {
				return ports.ErrConflict
			}
			out.BaseContent = target.Content
			out.TeamIDs = target.Teams
		}
		separator := 0
		if in.TargetID != nil {
			separator = 2
		}
		if len(out.BaseContent)+len(in.Content)+separator > MaxMarkdownBytes {
			return ports.ErrInvalid
		}
		out.Revision++
		raw, _ := json.Marshal(out) // Only strings, integers and nullable scalar fields.
		_, e = tx.ExecContext(ctx, `INSERT INTO resolution_drafts(question_id,author_id,revision,payload) VALUES($1,$2,$3,$4) ON CONFLICT(question_id,author_id) DO UPDATE SET revision=EXCLUDED.revision,payload=EXCLUDED.payload,updated_at=now()`, id, p.UserID, out.Revision, string(raw))
		return e
	})
	return out, uploadError(err)
}
