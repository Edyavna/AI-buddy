package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
)

type hrJob struct {
	Document
	Publisher string
	Claim     string
}

func (w UploadWorker) runHR(ctx context.Context) (bool, error) {
	var j hrJob
	err := w.Knowledge.DB.QueryRowContext(ctx, `UPDATE hr_document_versions v
SET claim=gen_random_uuid(),lease_until=now()+interval '6 minutes'
WHERE (v.document_id,v.version)=(SELECT v.document_id,v.version FROM hr_document_versions v
JOIN hr_documents d ON d.id=v.document_id AND d.latest_version=v.version
WHERE v.status='indexing' AND (v.lease_until IS NULL OR v.lease_until<=now())
ORDER BY v.updated_at,v.document_id LIMIT 1 FOR UPDATE OF v SKIP LOCKED)
RETURNING v.document_id::text,v.version,v.title,v.content,v.publisher_id,v.claim::text`).Scan(&j.ID, &j.Version, &j.Title, &j.Content, &j.Publisher, &j.Claim)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	j.Direction, j.Source, j.SourceID = "hr", "hr_upload", j.ID
	indexCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	err = database.WithinTx(indexCtx, w.Knowledge.DB, func(tx *sql.Tx) error {
		teams, err := versionTeams(indexCtx, tx, j.ID, j.Version)
		if err != nil {
			return err
		}
		return checkHRAudience(indexCtx, tx, j.Publisher, teams)
	})
	if err == nil {
		var chunks, vectors []string
		chunks, vectors, err = w.Knowledge.prepareIndex(indexCtx, j.Document)
		if err == nil {
			err = w.publishHR(indexCtx, j, chunks, vectors)
		}
	}
	if err != nil && ctx.Err() == nil {
		_, err = w.Knowledge.DB.ExecContext(ctx, `UPDATE hr_document_versions SET status='failed',claim=NULL,lease_until=NULL,
error='Не удалось опубликовать документ. Проверьте доступ и повторите попытку.',updated_at=now()
WHERE document_id=$1 AND version=$2 AND claim=$3`, j.ID, j.Version, j.Claim)
	}
	return true, err
}

func (w UploadWorker) publishHR(ctx context.Context, j hrJob, chunks, vectors []string) error {
	return database.WithinTx(ctx, w.Knowledge.DB, func(tx *sql.Tx) error {
		var latest int64
		if err := tx.QueryRowContext(ctx, `SELECT latest_version FROM hr_documents WHERE id=$1 FOR UPDATE`, j.ID).Scan(&latest); err != nil {
			return err
		}
		if latest != j.Version {
			return nil
		}
		teams, err := versionTeams(ctx, tx, j.ID, j.Version)
		if err != nil {
			return err
		}
		if err := checkHRAudience(ctx, tx, j.Publisher, teams); err != nil {
			return err
		}
		j.UpdatedAt = time.Now().UTC()
		result, err := tx.ExecContext(ctx, `UPDATE hr_document_versions SET status='ready',claim=NULL,lease_until=NULL,updated_at=$4
WHERE document_id=$1 AND version=$2 AND claim=$3 AND status='indexing'`, j.ID, j.Version, j.Claim, j.UpdatedAt)
		if err != nil {
			return err
		}
		if count, err := result.RowsAffected(); err != nil || count == 0 {
			return err
		}
		j.TeamID = teams[0]
		if err := writeIndex(ctx, tx, j.Document, contentDigest(j.Document), chunks, vectors); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE hr_documents SET published_version=$2,revoked=false WHERE id=$1`, j.ID, j.Version)
		return err
	})
}
