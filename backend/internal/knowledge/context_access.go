package knowledge

import (
	"context"
	"database/sql"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// ReadContextDocumentTx applies the ordinary reader ACL to the intern, then
// checks the recipient as well. The caller has authorized the question and
// locked its team/assignment. Only the explicitly shared context is exposed.
func ReadContextDocumentTx(ctx context.Context, tx *sql.Tx, student, viewer identity.Principal, id, direction string) (ports.Document, error) {
	var ignored string
	// Revocation changes the HR row without replacing the search index.
	err := tx.QueryRowContext(ctx, `SELECT id::text FROM hr_documents WHERE id::text=$1 FOR SHARE`, id).Scan(&ignored)
	if err != nil && err != sql.ErrNoRows {
		return ports.Document{}, err
	}
	var d ports.Document
	err = tx.QueryRowContext(ctx, `SELECT d.id,d.title,d.content,d.updated_at,d.direction,d.version FROM knowledge_documents d
 WHERE `+readableDocument+` AND d.id=$4 AND d.direction=$5
 AND ($6='intern' OR ($6='mentor' AND d.direction='mentor' AND (d.users='[]'::jsonb OR d.users ? $7))
 OR ($6='hr' AND d.direction='hr' AND EXISTS(SELECT 1 FROM hr_team_assignments h WHERE h.hr_user_id=$7 AND h.team_id=$1 AND h.active))) FOR SHARE OF d`, student.TeamID, student.UserID, student.Role, id, direction, viewer.Role, viewer.UserID).Scan(&d.ID, &d.Title, &d.Content, &d.UpdatedAt, &d.Direction, &d.Version)
	return d, err
}
