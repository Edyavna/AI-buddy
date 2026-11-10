package testkit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

// Postgres creates an isolated database, applies real migrations and registers
// cleanup. A configured but unreachable database is a failure, never a skip.
func Postgres(t *testing.T) *sql.DB {
	t.Helper()
	db := UnmigratedPostgres(t)
	if err := migrations.Apply(context.Background(), db, migrations.Files); err != nil {
		t.Fatal(err)
	}
	return db
}

// UnmigratedPostgres also isolates modules that own fixed schemas.
// TEST_DATABASE_URL must name a disposable test server with CREATEDB permission.
func UnmigratedPostgres(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required in CI")
		}
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "buddy_test_" + strings.ToLower(rand.Text())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+schema+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Del("search_path")
	u.RawQuery = q.Encode()
	u.Path = "/" + schema
	db, err := database.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
