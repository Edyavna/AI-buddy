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

type HRDocument struct {
	ID        string    `json:"id"`
	AuthorID  string    `json:"author_id"`
	Direction string    `json:"direction"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Teams     []string  `json:"team_ids"`
	Version   int64     `json:"version"`
	Published *int64    `json:"published_version"`
	Revoked   bool      `json:"revoked"`
	Status    string    `json:"status"`
	Error     *string   `json:"error"`
	Updated   time.Time `json:"updated_at"`
}

type HRDocumentInput struct {
	Operation string   `json:"operation_id"`
	Version   int64    `json:"version"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Teams     []string `json:"team_ids"`
}

var errDocumentVersion = errors.New("document version changed")

const hrDocumentSelect = `SELECT d.id::text,d.author_id,v.title,v.content,v.version,d.published_version,d.revoked,v.status,v.error,v.updated_at,
COALESCE((SELECT jsonb_agg(t.team_id ORDER BY t.team_id) FROM hr_document_teams t WHERE t.document_id=d.id AND t.version=v.version),'[]')
FROM hr_documents d JOIN hr_document_versions v ON (v.document_id,v.version)=(d.id,d.latest_version)`

// Editors must cover the entire current audience, including the published version.
const hrEditor = `NOT EXISTS (SELECT 1 FROM hr_document_teams t
WHERE t.document_id=d.id AND (t.version=d.latest_version OR (NOT d.revoked AND t.version=d.published_version))
AND NOT EXISTS (SELECT 1 FROM hr_team_assignments h WHERE h.hr_user_id=$1 AND h.team_id=t.team_id AND h.active))`

func scanHRDocument(row uploadRow) (HRDocument, error) {
	d := HRDocument{Direction: "hr"}
	var teams []byte
	err := row.Scan(&d.ID, &d.AuthorID, &d.Title, &d.Content, &d.Version, &d.Published, &d.Revoked, &d.Status, &d.Error, &d.Updated, &teams)
	if err != nil {
		return d, err
	}
	d.Updated = d.Updated.UTC()
	return d, json.Unmarshal(teams, &d.Teams)
}

func hrPrincipal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err == nil && p.Role != identity.HR {
		err = ports.ErrForbidden
	}
	return p, err
}

func (s Uploads) HRList(ctx context.Context) ([]HRDocument, error) {
	p, err := hrPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, hrDocumentSelect+" WHERE "+hrEditor+" ORDER BY v.updated_at DESC,d.id", p.UserID)
	if err != nil {
		return nil, uploadError(err)
	}
	defer rows.Close()
	items := []HRDocument{}
	for rows.Next() {
		d, err := scanHRDocument(rows)
		if err != nil {
			return nil, uploadError(err)
		}
		items = append(items, d)
	}
	return items, uploadError(rows.Err())
}

func (s Uploads) HRGet(ctx context.Context, id string) (HRDocument, error) {
	p, err := hrPrincipal(ctx)
	if err != nil {
		return HRDocument{}, err
	}
	if !uuidPattern.MatchString(id) {
		return HRDocument{}, ports.ErrNotFound
	}
	d, err := scanHRDocument(s.DB.QueryRowContext(ctx, hrDocumentSelect+" WHERE "+hrEditor+" AND d.id=$2", p.UserID, id))
	return d, uploadError(err)
}

func checkHRAudience(ctx context.Context, tx *sql.Tx, hr string, teams []string) error {
	for _, team := range teams {
		if err := hrscope.CheckTeam(ctx, tx, hr, team); err != nil {
			return err
		}
	}
	return nil
}

func versionTeams(ctx context.Context, tx *sql.Tx, id string, version int64) ([]string, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(team_id ORDER BY team_id),'[]') FROM hr_document_teams WHERE document_id=$1 AND version=$2`, id, version).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var teams []string
	return teams, json.Unmarshal(raw, &teams)
}

func lockEditor(ctx context.Context, tx *sql.Tx, hr, id string) (HRDocument, error) {
	d, err := scanHRDocument(tx.QueryRowContext(ctx, hrDocumentSelect+" WHERE "+hrEditor+" AND d.id=$2 FOR UPDATE OF d", hr, id))
	if err != nil {
		return d, err
	}
	teams := d.Teams
	if !d.Revoked && d.Published != nil && *d.Published != d.Version {
		published, err := versionTeams(ctx, tx, id, *d.Published)
		if err != nil {
			return d, err
		}
		teams = append(append([]string{}, teams...), published...)
		slices.Sort(teams)
		teams = slices.Compact(teams)
	}
	return d, checkHRAudience(ctx, tx, hr, teams)
}

func (s Uploads) HRSave(ctx context.Context, id string, in HRDocumentInput) (HRDocument, error) {
	p, err := hrPrincipal(ctx)
	if err != nil {
		return HRDocument{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 || !validText(in.Title) ||
		len(in.Content) > MaxMarkdownBytes || strings.TrimSpace(strings.TrimPrefix(in.Content, "\uFEFF")) == "" || !validText(in.Content) ||
		len(in.Teams) == 0 || len(in.Teams) > 100 {
		return HRDocument{}, ports.ErrInvalid
	}
	slices.Sort(in.Teams)
	in.Teams = slices.Compact(in.Teams)
	for _, team := range in.Teams {
		if team == "" || !validText(team) {
			return HRDocument{}, ports.ErrInvalid
		}
	}
	var d HRDocument
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := checkHRAudience(ctx, tx, p.UserID, in.Teams); err != nil {
			return err
		}
		version := int64(1)
		if id == "" {
			if !uuidPattern.MatchString(in.Operation) {
				return ports.ErrInvalid
			}
			if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(628115,hashtext($1::text || ':' || lower($2::text)))`, p.UserID, in.Operation); err != nil {
				return err
			}
			raw, err := json.Marshal(in)
			if err != nil {
				return err
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(raw))
			var old string
			err = tx.QueryRowContext(ctx, `SELECT id::text,create_hash FROM hr_documents WHERE author_id=$1 AND create_id=$2`, p.UserID, in.Operation).Scan(&id, &old)
			if err == nil {
				if old != hash {
					return ports.ErrConflict
				}
				d, err = lockEditor(ctx, tx, p.UserID, id)
				return err
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err := tx.QueryRowContext(ctx, `INSERT INTO hr_documents(author_id,create_id,create_hash) VALUES($1,$2,$3) RETURNING id::text`, p.UserID, in.Operation, hash).Scan(&id); err != nil {
				return err
			}
		} else {
			if !uuidPattern.MatchString(id) {
				return ports.ErrNotFound
			}
			old, err := lockEditor(ctx, tx, p.UserID, id)
			if err != nil {
				return err
			}
			if in.Version != old.Version {
				return errDocumentVersion
			}
			version = old.Version + 1
			if _, err := tx.ExecContext(ctx, `UPDATE hr_document_versions SET status='draft',claim=NULL,lease_until=NULL,error=NULL WHERE document_id=$1 AND version=$2 AND status='indexing'`, id, old.Version); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE hr_documents SET latest_version=$2 WHERE id=$1`, id, version); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO hr_document_versions(document_id,version,title,content) VALUES($1,$2,$3,$4)`, id, version, in.Title, in.Content); err != nil {
			return err
		}
		for _, team := range in.Teams {
			if _, err := tx.ExecContext(ctx, `INSERT INTO hr_document_teams(document_id,version,team_id) VALUES($1,$2,$3)`, id, version, team); err != nil {
				return err
			}
		}
		d, err = scanHRDocument(tx.QueryRowContext(ctx, hrDocumentSelect+" WHERE d.id=$1", id))
		return err
	})
	return d, hrDocumentError(err)
}

func hrDocumentError(err error) error {
	if errors.Is(err, errDocumentVersion) {
		return err
	}
	return uploadError(err)
}

func (s Uploads) HRPublish(ctx context.Context, id string, version int64, action string) (HRDocument, error) {
	p, err := hrPrincipal(ctx)
	if err != nil {
		return HRDocument{}, err
	}
	if !uuidPattern.MatchString(id) {
		return HRDocument{}, ports.ErrNotFound
	}
	var d HRDocument
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		d, err = lockEditor(ctx, tx, p.UserID, id)
		if err != nil {
			return err
		}
		if version != d.Version {
			return errDocumentVersion
		}
		switch action {
		case "revoke":
			if _, err := tx.ExecContext(ctx, `UPDATE hr_documents SET revoked=true WHERE id=$1`, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE hr_document_versions SET status='draft',claim=NULL,lease_until=NULL,error=NULL WHERE document_id=$1 AND version=$2 AND status='indexing'`, id, version); err != nil {
				return err
			}
		case "publish", "retry":
			if d.Status == "indexing" || (d.Status == "ready" && !d.Revoked) {
				return nil
			}
			if action == "retry" && d.Status != "failed" {
				return ports.ErrConflict
			}
			if _, err := tx.ExecContext(ctx, `UPDATE hr_document_versions SET status='indexing',publisher_id=$3,error=NULL,updated_at=now() WHERE document_id=$1 AND version=$2`, id, version, p.UserID); err != nil {
				return err
			}
		default:
			return ports.ErrInvalid
		}
		d, err = scanHRDocument(tx.QueryRowContext(ctx, hrDocumentSelect+" WHERE d.id=$1", id))
		return err
	})
	return d, hrDocumentError(err)
}

const documentDirection = `CASE WHEN pb.document_id IS NULL THEN d.direction ELSE '' END`

const readableDocument = `((pb.document_id IS NOT NULL AND $3='intern')
OR ($3='intern' AND pb.document_id IS NULL AND d.direction='mentor' AND d.team_id=$1 AND (d.users='[]'::jsonb OR d.users ? $2))
OR (d.direction='hr' AND EXISTS(SELECT 1 FROM hr_documents doc JOIN hr_document_teams t
 ON t.document_id=doc.id AND t.version=doc.published_version
 WHERE doc.id::text=d.id AND doc.published_version=d.version AND NOT doc.revoked
 AND (($3='intern' AND t.team_id=$1) OR ($3='hr' AND EXISTS(SELECT 1 FROM hr_team_assignments h
 WHERE h.hr_user_id=$2 AND h.team_id=t.team_id AND h.active))))))`

func checkHRLink(ctx context.Context, tx *sql.Tx, hr, student identity.Principal, id string) error {
	if err := hrscope.CheckTeam(ctx, tx, hr.UserID, student.TeamID); err != nil {
		return uploadError(err)
	}
	var found int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM hr_documents doc JOIN knowledge_documents d ON d.id=doc.id::text
JOIN hr_document_teams t ON t.document_id=doc.id AND t.version=doc.published_version
WHERE doc.id=$1 AND NOT doc.revoked AND doc.published_version=d.version AND t.team_id=$2 FOR SHARE OF doc,d,t`, id, student.TeamID).Scan(&found)
	return uploadError(err)
}
