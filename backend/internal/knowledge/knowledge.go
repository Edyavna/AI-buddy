// Package knowledge imports normalized documents and enforces ACL before retrieval.
package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Embedder interface {
	Embed(context.Context, string, string) ([]float64, error)
}
type Document struct {
	ID, Source, SourceID, Title, Content, TeamID string
	Users                                        []string
	UpdatedAt                                    time.Time
}
type Service struct {
	DB            *sql.DB
	Embedder      Embedder
	MinSimilarity float64
}

func vector(v []float64) (string, error) {
	if len(v) != 1024 {
		return "", ports.ErrUnavailable
	}
	sum := 0.0
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", ports.ErrUnavailable
		}
		sum += x * x
	}
	if sum == 0 {
		return "", ports.ErrUnavailable
	}
	b, e := json.Marshal(v)
	return string(b), e
}
func (s *Service) Import(ctx context.Context, d Document) error {
	if d.ID == "" || d.Source == "" || d.SourceID == "" || d.Title == "" || d.TeamID == "" || d.UpdatedAt.IsZero() || len([]rune(d.Content)) == 0 || len([]rune(d.Content)) > 16000 {
		return ports.ErrInvalid
	}
	hash := sha256.Sum256([]byte(d.Title + "\n" + d.Content))
	digest := hex.EncodeToString(hash[:])
	var old string
	err := s.DB.QueryRowContext(ctx, `SELECT hash FROM knowledge_documents WHERE id=$1`, d.ID).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ports.ErrUnavailable
	}
	chunks := []string{}
	vectors := []string{}
	if old != digest {
		r := []rune(d.Content)
		for start := 0; start < len(r); start += 1000 {
			chunk := string(r[start:min(start+1200, len(r))])
			v, e := s.Embedder.Embed(ctx, "retrieval.passage", d.Title+"\n"+chunk)
			if e != nil {
				return ports.ErrUnavailable
			}
			encoded, e := vector(v)
			if e != nil {
				return e
			}
			chunks = append(chunks, chunk)
			vectors = append(vectors, encoded)
		}
	}
	users := d.Users
	if users == nil {
		users = []string{}
	}
	acl, _ := json.Marshal(users)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return ports.ErrUnavailable
	}
	defer tx.Rollback()
	var current string
	lockErr := tx.QueryRowContext(ctx, `SELECT hash FROM knowledge_documents WHERE id=$1 FOR UPDATE`, d.ID).Scan(&current)
	if lockErr != nil && !errors.Is(lockErr, sql.ErrNoRows) {
		return ports.ErrUnavailable
	}
	if current != old {
		return ports.ErrConflict
	}
	// ACL is always replaced even when the content hash did not change.
	_, e = tx.ExecContext(ctx, `INSERT INTO knowledge_documents(id,source,source_id,title,content,updated_at,team_id,users,hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET source=EXCLUDED.source,source_id=EXCLUDED.source_id,title=EXCLUDED.title,content=EXCLUDED.content,updated_at=EXCLUDED.updated_at,team_id=EXCLUDED.team_id,users=EXCLUDED.users,hash=EXCLUDED.hash`, d.ID, d.Source, d.SourceID, d.Title, d.Content, d.UpdatedAt, d.TeamID, string(acl), digest)
	if e != nil {
		return ports.ErrUnavailable
	}
	if old != digest {
		if _, e = tx.ExecContext(ctx, `DELETE FROM knowledge_chunks WHERE document_id=$1`, d.ID); e != nil {
			return ports.ErrUnavailable
		}
		for i, chunk := range chunks {
			if _, e = tx.ExecContext(ctx, `INSERT INTO knowledge_chunks VALUES($1,$2,$3,$4::vector)`, fmt.Sprintf("%s:%d", d.ID, i), d.ID, chunk, vectors[i]); e != nil {
				return ports.ErrUnavailable
			}
		}
	}
	if tx.Commit() != nil {
		return ports.ErrUnavailable
	}
	return nil
}
func (s *Service) Search(ctx context.Context, q string) ([]ports.Source, error) {
	p, e := identity.Require(ctx)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(q) == "" || len([]rune(q)) > 2000 {
		return nil, ports.ErrInvalid
	}
	v, e := s.Embedder.Embed(ctx, "retrieval.query", q)
	if e != nil {
		return nil, ports.ErrUnavailable
	}
	encoded, e := vector(v)
	if e != nil {
		return nil, e
	}
	threshold := s.MinSimilarity
	if threshold == 0 {
		threshold = 0.35
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT c.id,d.id,d.title,c.excerpt FROM knowledge_chunks c JOIN knowledge_documents d ON d.id=c.document_id WHERE d.team_id=$1 AND (d.users='[]'::jsonb OR d.users ? $2) AND 1-(c.embedding <=> $3::vector)>=$4 ORDER BY c.embedding <=> $3::vector,c.id LIMIT 5`, p.TeamID, p.UserID, encoded, threshold)
	if e != nil {
		return nil, ports.ErrUnavailable
	}
	defer rows.Close()
	out := []ports.Source{}
	for rows.Next() {
		var x ports.Source
		if rows.Scan(&x.ID, &x.DocumentID, &x.Title, &x.Excerpt) != nil {
			return nil, ports.ErrUnavailable
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		return nil, ports.ErrUnavailable
	}
	return out, nil
}
func (s *Service) Read(ctx context.Context, id string) (ports.Document, error) {
	p, e := identity.Require(ctx)
	if e != nil {
		return ports.Document{}, e
	}
	var d ports.Document
	e = s.DB.QueryRowContext(ctx, `SELECT id,title,content,updated_at FROM knowledge_documents WHERE id=$1 AND team_id=$2 AND (users='[]'::jsonb OR users ? $3)`, id, p.TeamID, p.UserID).Scan(&d.ID, &d.Title, &d.Content, &d.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return d, ports.ErrNotFound
	}
	if e != nil {
		return d, ports.ErrUnavailable
	}
	return d, nil
}

// LoadMarkdown is only a loader: metadata is supplied by a portable manifest.
func LoadMarkdown(dir string) ([]Document, error) {
	data, e := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return nil, e
	}
	var docs []Document
	if json.Unmarshal(data, &docs) != nil {
		return nil, ports.ErrInvalid
	}
	for i := range docs {
		d := &docs[i]
		if filepath.Base(d.SourceID) != d.SourceID || !strings.HasSuffix(d.SourceID, ".md") {
			return nil, ports.ErrInvalid
		}
		b, e := os.ReadFile(filepath.Join(dir, d.SourceID))
		if e != nil {
			return nil, e
		}
		d.Content = string(b)
	}
	return docs, nil
}
func Handler(k ports.Knowledge) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, e := k.Read(r.Context(), r.PathValue("document_id"))
		if e != nil {
			status, code := 503, "SERVICE_UNAVAILABLE"
			if errors.Is(e, ports.ErrNotFound) {
				status, code = 404, "NOT_FOUND"
			}
			if errors.Is(e, identity.ErrUnauthenticated) {
				status, code = 401, "UNAUTHENTICATED"
			}
			httpapi.WriteError(w, r, &httpapi.Error{Status: status, Code: code, Message: "Документ недоступен"})
			return
		}
		httpapi.WriteJSON(w, 200, struct {
			ID        string    `json:"id"`
			Title     string    `json:"title"`
			Content   string    `json:"content"`
			UpdatedAt time.Time `json:"updated_at"`
		}{d.ID, d.Title, d.Content, d.UpdatedAt})
	}
}
