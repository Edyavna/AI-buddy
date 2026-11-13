package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
)

type UploadWorker struct{ Knowledge *Service }

// Recover runs after the API acquires its exclusive runtime lock. A stopped
// process cannot still be working on any of these jobs.
func (w UploadWorker) Recover(ctx context.Context) error {
	_, err := w.Knowledge.DB.ExecContext(ctx, `UPDATE mentor_documents SET claim=NULL, lease_until=NULL
		WHERE status='processing' AND claim IS NOT NULL`)
	if err != nil {
		return err
	}
	_, err = w.Knowledge.DB.ExecContext(ctx, `UPDATE hr_document_versions SET claim=NULL,lease_until=NULL WHERE status='indexing' AND claim IS NOT NULL`)
	return err
}

func (w UploadWorker) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		processed, err := w.RunOne(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Warn("could not process queued Markdown document")
		}
		if processed && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (w UploadWorker) RunOne(ctx context.Context) (bool, error) {
	if processed, err := w.runHR(ctx); processed || err != nil {
		return processed, err
	}
	var d Document
	var claim string
	err := w.Knowledge.DB.QueryRowContext(ctx, `UPDATE mentor_documents d SET
		claim=gen_random_uuid(), lease_until=clock_timestamp()+interval '6 minutes'
		WHERE d.id=(SELECT id FROM mentor_documents WHERE status='processing'
			AND (lease_until IS NULL OR lease_until<=clock_timestamp()) ORDER BY updated_at,id
			LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING d.id::text,d.title,d.content,d.team_id,d.claim::text`).Scan(&d.ID, &d.Title, &d.Content, &d.TeamID, &claim)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	d.Source, d.SourceID, d.UpdatedAt = "mentor_upload", d.ID, time.Now().UTC()
	indexCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	chunks, vectors, err := w.Knowledge.prepareIndex(indexCtx, d)
	if err == nil {
		err = w.publish(indexCtx, d, claim, chunks, vectors)
	}
	if err != nil && ctx.Err() == nil {
		_, err = w.Knowledge.DB.ExecContext(ctx, `UPDATE mentor_documents SET status='failed',
			error='Не удалось проиндексировать документ. Повторите попытку.',updated_at=clock_timestamp(),
			claim=NULL,lease_until=NULL WHERE id=$1 AND claim=$2`, d.ID, claim)
	}
	return true, err
}

func (w UploadWorker) publish(ctx context.Context, d Document, claim string, chunks, vectors []string) error {
	return database.WithinTx(ctx, w.Knowledge.DB, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE mentor_documents SET status='ready',
			error=NULL,updated_at=$3,claim=NULL,lease_until=NULL WHERE id=$1 AND claim=$2`, d.ID, claim, d.UpdatedAt)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count == 0 {
			return err
		}
		return writeIndex(ctx, tx, d, contentDigest(d), chunks, vectors)
	})
}
