package knowledge

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

const (
	playbookSource   = "avito-tech/playbook"
	playbookIDPrefix = "avito-playbook-"
)

type PlaybookSyncResult struct {
	Indexed, Reused, Archived int
}

type playbookIndexEntry struct {
	path, blob string
	part       int
}

type preparedPlaybookPart struct {
	part            playbookPart
	chunks, vectors []string
}

// SyncPlaybook prepares changed files outside the transaction. Archived rows
// remain readable through the immutable document IDs carried by old answers.
func (s *Service) SyncPlaybook(ctx context.Context, snapshot PlaybookSnapshot) (PlaybookSyncResult, error) {
	var result PlaybookSyncResult
	if snapshot.Commit == "" || len(snapshot.parts) == 0 {
		return result, ports.ErrInvalid
	}
	active, generation, err := s.playbookIndex(ctx)
	if err != nil {
		return result, err
	}
	var pending []preparedPlaybookPart
	var activate []string
	for _, part := range snapshot.parts {
		key := playbookIndexEntry{path: part.path, part: part.number, blob: part.blob}
		if _, ok := active[key]; ok {
			delete(active, key)
			result.Reused++
			continue
		}
		var exists bool
		err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM playbook_documents WHERE document_id=$1)`, part.doc.ID).Scan(&exists)
		if err != nil {
			return PlaybookSyncResult{}, fmt.Errorf("check archived Playbook part: %w", err)
		}
		if exists {
			activate = append(activate, part.doc.ID)
			result.Reused++
			continue
		}
		chunks, vectors, err := s.prepareIndex(ctx, part.doc)
		if err != nil {
			return PlaybookSyncResult{}, fmt.Errorf("embed Playbook %s, part %d: %w", part.path, part.number, err)
		}
		pending = append(pending, preparedPlaybookPart{part, chunks, vectors})
		result.Indexed++
	}
	result.Archived = len(active)
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var current int64
		var currentCommit string
		err := tx.QueryRowContext(ctx, `SELECT generation,commit_sha FROM playbook_index WHERE singleton FOR UPDATE`).Scan(&current, &currentCommit)
		if err != nil {
			return err
		}
		if current != generation {
			return ports.ErrConflict
		}
		for _, id := range active {
			if _, err := tx.ExecContext(ctx, `UPDATE playbook_documents SET active=false WHERE document_id=$1`, id); err != nil {
				return err
			}
		}
		for _, id := range activate {
			if _, err := tx.ExecContext(ctx, `UPDATE playbook_documents SET active=true WHERE document_id=$1`, id); err != nil {
				return err
			}
		}
		for _, item := range pending {
			p := item.part
			if err := writeIndex(ctx, tx, p.doc, contentDigest(p.doc), item.chunks, item.vectors); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO playbook_documents
				(document_id,path,part,commit_sha,blob_sha,original_url,active)
				VALUES($1,$2,$3,$4,$5,$6,true)`, p.doc.ID, p.path, p.number, snapshot.Commit, p.blob, p.url)
			if err != nil {
				return err
			}
		}
		if currentCommit == snapshot.Commit && len(active)+len(activate)+len(pending) == 0 {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE playbook_index SET generation=generation+1,commit_sha=$1 WHERE singleton`, snapshot.Commit)
		return err
	})
	if err != nil {
		return PlaybookSyncResult{}, fmt.Errorf("publish Playbook index: %w", err)
	}
	return result, nil
}

func (s *Service) playbookIndex(ctx context.Context) (map[playbookIndexEntry]string, int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT i.generation,p.document_id,p.path,p.part,p.blob_sha
FROM playbook_index i LEFT JOIN playbook_documents p ON p.active WHERE i.singleton`)
	if err != nil {
		return nil, 0, fmt.Errorf("read Playbook index: %w", err)
	}
	defer rows.Close()
	active := make(map[playbookIndexEntry]string)
	var generation int64
	for rows.Next() {
		var id, path, blob sql.NullString
		var part sql.NullInt64
		if err := rows.Scan(&generation, &id, &path, &part, &blob); err != nil {
			return nil, 0, err
		}
		if id.Valid {
			active[playbookIndexEntry{path: path.String, part: int(part.Int64), blob: blob.String}] = id.String
		}
	}
	return active, generation, rows.Err()
}
