package migrations_test

import (
	"context"

	"database/sql"
	"errors"
	"io/fs"

	"sync"
	"testing"
	"testing/fstest"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func testDB(t *testing.T) *sql.DB { return testkit.UnmigratedPostgres(t) }

func TestPostgresMigrationLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := migrations.Verify(ctx, db, migrations.Files); err == nil {
		t.Fatal("empty DB accepted")
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := migrations.Apply(ctx, db, migrations.Files); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := migrations.Verify(ctx, db, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var count int
	entries, err := fs.ReadDir(migrations.Files, "sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM buddy_schema_migrations").Scan(&count); err != nil || count != len(entries) {
		t.Fatalf("journal=%d: %v", count, err)
	}
	source := fstest.MapFS{}
	for _, entry := range entries {
		data, err := fs.ReadFile(migrations.Files, "sql/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		source["sql/"+entry.Name()] = &fstest.MapFile{Data: data}
	}
	source["sql/999999_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE must_rollback(id integer); SELECT missing_column;")}
	if err := migrations.Apply(ctx, db, source); err == nil {
		t.Fatal("invalid SQL accepted")
	}
	var tableExists bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('must_rollback') IS NOT NULL").Scan(&tableExists); err != nil || tableExists {
		t.Fatalf("DDL escaped rollback: %v", err)
	}
	delete(source, "sql/999999_broken.sql")
	source["sql/000001_platform.sql"] = &fstest.MapFile{Data: []byte("SELECT 2;")}
	if err := migrations.Apply(ctx, db, source); err == nil {
		t.Fatal("changed applied migration accepted")
	}
}

func TestPostgresUnitOfWork(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE atomic_pair(id integer PRIMARY KEY, kind text)"); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("second write failed")
	err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO atomic_pair VALUES(1,'draft')"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic swallowed")
			}
		}()
		_ = database.WithinTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO atomic_pair VALUES(2,'draft')"); err != nil {
				t.Fatal(err)
			}
			panic("rollback")
		})
	}()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM atomic_pair").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial commit: %d %v", count, err)
	}
	if err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO atomic_pair VALUES(3,'draft'),(4,'link')")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM atomic_pair").Scan(&count); err != nil || count != 2 {
		t.Fatalf("commit missing: %d %v", count, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := database.WithinTx(canceled, db, func(*sql.Tx) error { t.Fatal("called with canceled context"); return nil }); err == nil {
		t.Fatal("canceled transaction accepted")
	}
}
