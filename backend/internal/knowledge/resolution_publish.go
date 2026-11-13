package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func resolutionState(ctx context.Context, tx *sql.Tx, p identity.Principal, q ResolutionSource, d *ResolutionDraft) error {
	if p.Role == identity.HR {
		doc, err := lockEditor(ctx, tx, p.UserID, *d.DocumentID)
		if err != nil {
			return err
		}
		if doc.Revoked || doc.Version != *d.DocumentVersion {
			return ports.ErrNotFound
		}
		d.Status, d.Error = doc.Status, doc.Error
	} else {
		var version int64
		err := tx.QueryRowContext(ctx, `SELECT status,error,version FROM mentor_documents WHERE id=$1 AND author_id=$2 AND team_id=$3`, *d.DocumentID, p.UserID, q.TeamID).Scan(&d.Status, &d.Error, &version)
		if err != nil {
			return err
		}
		if version != *d.DocumentVersion {
			return ports.ErrNotFound
		}
		if d.Status == "processing" {
			d.Status = "indexing"
		}
	}
	if d.Status == "ready" {
		student := identity.Principal{UserID: q.StudentID, TeamID: q.TeamID, Role: identity.Intern}
		return (Uploads{}).CheckReady(ctx, tx, p, student, []string{*d.DocumentID})
	}
	return nil
}

// The draft row and destination version are committed with the durable indexing
// job. Repeating publish after a lost response reads the same publication.
func (s Uploads) PublishResolution(ctx context.Context, id string, revision int64, reviewed, retry bool) (ResolutionDraft, error) {
	var out ResolutionDraft
	p, err := identity.Require(ctx)
	if err != nil {
		return out, err
	}
	if !reviewed || revision < 1 {
		return out, ports.ErrInvalid
	}
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		q, e := resolutionSource(ctx, tx, p, id)
		if e != nil {
			return e
		}
		d, e := readResolution(ctx, tx, id, p.UserID)
		if e != nil {
			return e
		}
		if d == nil {
			return ports.ErrNotFound
		}
		if d.Revision != revision {
			return ports.ErrConflict
		}
		if d.DocumentID != nil {
			if e = resolutionState(ctx, tx, p, q, d); e != nil {
				return e
			}
			if !retry || d.Status != "failed" {
				out = *d
				return nil
			}
			if q.Revision != d.Source.Revision {
				return ports.ErrConflict
			}
			if p.Role == identity.HR {
				_, e = tx.ExecContext(ctx, `UPDATE hr_document_versions SET status='indexing',error=NULL,claim=NULL,lease_until=NULL,updated_at=now() WHERE document_id=$1 AND version=$2`, *d.DocumentID, *d.DocumentVersion)
			} else {
				_, e = tx.ExecContext(ctx, `UPDATE mentor_documents SET status='processing',error=NULL,claim=NULL,lease_until=NULL,updated_at=now() WHERE id=$1`, *d.DocumentID)
			}
			if e != nil {
				return e
			}
			d.Status, d.Error = "indexing", nil
			out = *d
			return nil
		}
		if retry || q.Revision != d.Source.Revision || strings.TrimSpace(d.Content) == "" {
			return ports.ErrConflict
		}
		// Detected credentials cannot be explicitly or accidentally published. Other
		// personal information still requires the human review recorded by reviewed.
		if hasResolutionSecret(d.Title + "\n" + d.Content) {
			return ports.ErrInvalid
		}
		var target ResolutionTarget
		if d.TargetID != nil {
			target, e = resolutionTarget(ctx, tx, p, q, *d.TargetID)
			if e != nil {
				return e
			}
			if target.Version != *d.BaseVersion {
				return ports.ErrConflict
			}
		}
		content := d.Content
		if d.TargetID != nil {
			content = target.Content + "\n\n" + d.Content
		}
		if hasResolutionSecret(content) {
			return ports.ErrInvalid
		}
		doc, version, e := queueResolution(ctx, tx, p, q, *d, target, content)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE resolution_drafts SET document_id=$3,document_version=$4,updated_at=now() WHERE question_id=$1 AND author_id=$2`, id, p.UserID, doc, version)
		if e != nil {
			return e
		}
		d.DocumentID, d.DocumentVersion, d.Status = &doc, &version, "indexing"
		out = *d
		return nil
	})
	return out, uploadError(err)
}

func queueResolution(ctx context.Context, tx *sql.Tx, p identity.Principal, q ResolutionSource, d ResolutionDraft, target ResolutionTarget, content string) (string, int64, error) {
	id := target.ID
	version := target.Version + 1
	if p.Role == identity.HR {
		teams := target.Teams
		if id == "" {
			teams = []string{q.TeamID}
			if err := tx.QueryRowContext(ctx, `INSERT INTO hr_documents(author_id,create_id,create_hash) VALUES($1,gen_random_uuid(),'resolution') RETURNING id::text`, p.UserID).Scan(&id); err != nil {
				return "", 0, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE hr_documents SET latest_version=$2 WHERE id=$1`, id, version); err != nil {
				return "", 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO hr_document_versions(document_id,version,title,content,status,publisher_id) VALUES($1,$2,$3,$4,'indexing',$5)`, id, version, d.Title, content, p.UserID); err != nil {
			return "", 0, err
		}
		for _, team := range teams {
			if _, err := tx.ExecContext(ctx, `INSERT INTO hr_document_teams VALUES($1,$2,$3)`, id, version, team); err != nil {
				return "", 0, err
			}
		}
	} else {
		if id == "" {
			if err := tx.QueryRowContext(ctx, `INSERT INTO mentor_documents(author_id,team_id,upload_id,question_id,title,content) VALUES($1,$2,gen_random_uuid(),$3,$4,$5) RETURNING id::text`, p.UserID, q.TeamID, q.ID, d.Title, content).Scan(&id); err != nil {
				return "", 0, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `INSERT INTO mentor_document_versions SELECT id,version,title,content FROM mentor_documents WHERE id=$1 ON CONFLICT DO NOTHING`, id); err != nil {
				return "", 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE mentor_documents SET title=$2,content=$3,version=$4,status='processing',error=NULL,claim=NULL,lease_until=NULL,updated_at=now() WHERE id=$1`, id, d.Title, content, version); err != nil {
				return "", 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mentor_document_versions VALUES($1,$2,$3,$4)`, id, version, d.Title, content); err != nil {
			return "", 0, err
		}
	}
	return id, version, nil
}

// Called in the existing publication transaction, before making any new index
// visible. Source revocation aborts publication. The question link is exposed
// only once the document is ready, through the existing ACL-filtered question API.
func linkResolution(ctx context.Context, tx *sql.Tx, doc string, version int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT question_id::text,author_id,payload FROM resolution_drafts WHERE document_id=$1 AND document_version=$2`, doc, version)
	if err != nil {
		return err
	}
	type publication struct {
		id, user string
		draft    ResolutionDraft
	}
	items := []publication{}
	for rows.Next() {
		var item publication
		var raw []byte
		if err = rows.Scan(&item.id, &item.user, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &item.draft); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		p := identity.Principal{UserID: item.user, TeamID: item.draft.Source.TeamID, Role: identity.Role(item.draft.Source.Direction)}
		q, e := resolutionSource(ctx, tx, p, item.id)
		if e != nil {
			return e
		}
		if q.Revision != item.draft.Source.Revision {
			return ports.ErrConflict
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO knowledge_gap_history(question_id,revision,snapshot) SELECT id,revision,jsonb_build_object('event','instruction_published','document_id',$2::text,'document_version',$3::bigint) FROM knowledge_gaps WHERE id=$1`, item.id, doc, version); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE knowledge_gaps SET document_ids=CASE WHEN document_ids ? $2 THEN document_ids ELSE document_ids || jsonb_build_array($2::text) END,revision=revision+1,updated_at=now() WHERE id=$1`, item.id, doc); e != nil {
			return e
		}
	}
	return nil
}
