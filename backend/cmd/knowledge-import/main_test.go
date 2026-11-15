package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestWikipediaImportDatabaseConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "://invalid")
	if err := run("", "../../../demo/wikipedia"); err == nil || !strings.Contains(err.Error(), "could not connect") {
		t.Fatal(err)
	}
}

func TestPostgresWikipediaImportCommand(t *testing.T) {
	db := testkit.Postgres(t)
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	dsn.Path = "/" + name
	t.Setenv("DATABASE_URL", dsn.String())
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Input []string }
		if r.URL.Path != "/embeddings" || json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		vector := make([]float64, 1024)
		vector[0] = 1
		items := make([]map[string]any, len(request.Input))
		for i := range items {
			items[i] = map[string]any{"index": i, "embedding": vector}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
	}))
	defer service.Close()
	t.Setenv("EMBEDDING_BASE_URL", service.URL)
	t.Setenv("EMBEDDING_API_KEY", "test")
	if err := run("", t.TempDir()); err == nil || !strings.Contains(err.Error(), "read Wikipedia manifest") {
		t.Fatal(err)
	}
	const dir = "../../../demo/wikipedia"
	if err := run("", dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EMBEDDING_BASE_URL", "")
	if err := run("", dir); err != nil {
		t.Fatal("repeated command must work without an embedding provider", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM knowledge_documents`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
