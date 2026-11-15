package knowledge

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

const wikipediaSnapshot = "../../../demo/wikipedia"

func TestLoadWikipediaManifest(t *testing.T) {
	doc, err := LoadWikipedia(wikipediaSnapshot)
	if err != nil || doc.Version != 155526969 || !strings.Contains(doc.Content, "## История") ||
		!strings.Contains(doc.Content, "CC BY-SA 4.0") || !strings.Contains(doc.Content, "Участники русскоязычной Википедии") {
		t.Fatal(doc.ID, doc.Version, err)
	}
	for _, tc := range []struct {
		name, manifest, text, want string
	}{
		{"missing manifest", "", "", "read Wikipedia manifest"},
		{"malformed manifest", "{", "", "parse Wikipedia manifest"},
		{"wrong origin", `{"revision":10,"url":"https://example.org"}`, "", "invalid Wikipedia manifest"},
		{"missing content", testWikipediaManifest("данные", 10), "", "read Wikipedia article"},
		{"corrupted content", testWikipediaManifest("исходный текст", 10), "изменённый текст", "invalid Wikipedia article or checksum"},
		{"empty text", testWikipediaManifest(" ", 10), " ", "invalid Wikipedia article or checksum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWikipediaFixture(t, dir, tc.manifest, tc.text)
			if _, err := LoadWikipedia(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
		})
	}
}

func testWikipediaManifest(content string, revision int64) string {
	m := wikipediaManifest{
		Revision: revision, Date: time.Date(2026, 10, 10, 5, 13, 44, 0, time.UTC),
		URL: fmt.Sprintf("%s%d", wikipediaRevisionURL, revision), Authors: "Авторы Википедии",
		License: wikipediaLicenseURL, Changes: "Тестовая выгрузка.", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(content))),
	}
	encoded, _ := json.Marshal(m)
	return string(encoded)
}

func writeWikipediaFixture(t *testing.T, dir, manifest, content string) {
	t.Helper()
	for name, text := range map[string]string{"manifest.json": manifest, "avito.md": content} {
		if text != "" {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPostgresWikipediaImportSearchAndACL(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(t.Context())
	s := Service{DB: db, Embedder: &fakeEmbedder{fail: true}}
	wiki, err := LoadWikipedia(wikipediaSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	counts := func() (docs, chunks int) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM knowledge_documents WHERE id=$1),
			(SELECT count(*) FROM knowledge_chunks WHERE document_id=$1)`, wiki.ID).Scan(&docs, &chunks); err != nil {
			t.Fatal(err)
		}
		return docs, chunks
	}
	if err := s.ImportWikipedia(ctx, t.TempDir()); err == nil {
		t.Fatal("invalid manifest accepted")
	}
	if err := s.ImportWikipedia(ctx, wikipediaSnapshot); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if docs, chunks := counts(); docs != 0 || chunks != 0 {
		t.Fatalf("partial import: %d documents, %d chunks", docs, chunks)
	}
	if err := s.Import(ctx, wiki); !errors.Is(err, ports.ErrInvalid) {
		t.Fatalf("public namespace must be reserved: %v", err)
	}
	s.Embedder = &fakeEmbedder{}
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			if e := s.ImportWikipedia(ctx, wikipediaSnapshot); e != nil {
				t.Error(e)
			}
		})
	}
	group.Wait()
	docs, chunks := counts()
	if docs != 1 || chunks < 2 {
		t.Fatal(docs, chunks)
	}
	s.Embedder = nil
	if err := s.ImportWikipedia(ctx, wikipediaSnapshot); err != nil {
		t.Fatal("repeat import must not call the embedder", err)
	}
	if d, c := counts(); d != docs || c != chunks {
		t.Fatal(d, c)
	}
	modified := t.TempDir()
	writeWikipediaFixture(t, modified, testWikipediaManifest("другой текст", wiki.Version), "другой текст")
	if err := s.ImportWikipedia(ctx, modified); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("same revision cannot replace an existing source", err)
	}
	s.Embedder = &fakeEmbedder{}
	hits, err := s.SearchDirection(ctx, "история Авито", "public")
	if err != nil || len(hits) == 0 || hits[0].DocumentID != wiki.ID || hits[0].Kind != ports.SourceWikipedia {
		t.Fatal(hits, err)
	}
	for _, direction := range []string{"hr", "mentor"} {
		if hits, err := s.SearchDirection(ctx, "порядок оформления", direction); err != nil || len(hits) != 0 {
			t.Fatal(direction, hits, err)
		}
	}
	foreign := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "other", TeamID: "other", Role: identity.Intern})
	if doc, err := s.Read(foreign, wiki.ID); err != nil || doc.OriginalURL != wikipediaRevisionURL+wiki.SourceID || doc.Version != wiki.Version || doc.Kind != ports.SourceWikipedia {
		t.Fatal(doc.ID, doc.OriginalURL, err)
	}
	if _, err := s.Read(t.Context(), wiki.ID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}
