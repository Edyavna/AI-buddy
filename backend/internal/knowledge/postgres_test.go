package knowledge

import (
	"context"
	"errors"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeEmbedder struct{ fail bool }

func (f *fakeEmbedder) Embed(context.Context, string, string) ([]float64, error) {
	if f.fail {
		return nil, ports.ErrUnavailable
	}
	v := make([]float64, 1024)
	v[0] = 1
	return v, nil
}

// S05/S07: this exercises real pgvector/ACL, not a SQL mock or semantic quality.
func TestPostgresACLAndImport(t *testing.T) {
	db := testkit.UnmigratedPostgres(t)
	ctx := testkit.Context(context.Background())
	if _, e := db.ExecContext(ctx, Schema); e != nil {
		t.Fatal(e)
	}
	embed := &fakeEmbedder{}
	s := Service{DB: db, Embedder: embed}
	d := Document{ID: "doc_local_setup", Source: "md", SourceID: "setup.md", Title: "Setup", Content: "Синтетический документ запуска", TeamID: testkit.TeamID, UpdatedAt: time.Now().UTC()}
	for range 2 {
		if e := s.Import(ctx, d); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e := db.QueryRow(`SELECT count(*) FROM knowledge_documents`).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	hits, e := s.Search(ctx, "запуск")
	if e != nil || len(hits) != 1 {
		t.Fatal(hits, e)
	}
	if _, e = s.Read(ctx, d.ID); e != nil {
		t.Fatal(e)
	}
	d.Users = []string{"another_user"}
	if e = s.Import(ctx, d); e != nil {
		t.Fatal(e)
	}
	hits, e = s.Search(ctx, "запуск")
	if e != nil || len(hits) != 0 {
		t.Fatal(hits, e)
	}
	if _, e = s.Read(ctx, d.ID); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	req := httptest.NewRequest("GET", "/api/v1/documents/"+d.ID, nil).WithContext(ctx)
	req.SetPathValue("document_id", d.ID)
	w := httptest.NewRecorder()
	Handler(&s)(w, req)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	other := identity.WithPrincipal(context.Background(), identity.Principal{UserID: "another_user", TeamID: "foreign", Role: identity.Intern})
	if _, e = s.Read(other, d.ID); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	embed.fail = true
	if _, e = s.Search(ctx, "запуск"); !errors.Is(e, ports.ErrUnavailable) {
		t.Fatal(e)
	}
	d.Users = nil
	if e = s.Import(ctx, d); e != nil {
		t.Fatal("ACL-only import must not need embedding", e)
	}
	d.Content = "Изменено"
	if e = s.Import(ctx, d); !errors.Is(e, ports.ErrUnavailable) {
		t.Fatal(e)
	}
}
