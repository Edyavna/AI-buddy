package migrations_test

import (
	"io/fs"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func TestPostgresActionQueueUpgradeAfterConversationArchive(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	released := migrationsThrough(t, "000024_conversation_archive.sql")
	// Reconstruct released dev without the unmerged action-queue migration.
	delete(released, "sql/000023_action_queue.sql")
	if err := migrations.Apply(ctx, db, released); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO conversations(id,user_id,team_id,client_conversation_id,title,archived) VALUES ('queue-upgrade','intern_demo_1','team_demo_backend','22222222-2222-4222-8222-222222222222','Сохранённый архив',true)`); err != nil {
		t.Fatal(err)
	}
	var applied string
	if err := db.QueryRowContext(ctx, `SELECT applied_at::text FROM buddy_schema_migrations WHERE name='000024_conversation_archive.sql'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrations.Apply(ctx, db, migrations.Files); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrations.Verify(ctx, db, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var archived bool
	var title, kept string
	if err := db.QueryRowContext(ctx, `SELECT archived,title FROM conversations WHERE id='queue-upgrade'`).Scan(&archived, &title); err != nil || !archived || title != "Сохранённый архив" {
		t.Fatalf("archive changed: %v %q %v", archived, title, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT applied_at::text FROM buddy_schema_migrations WHERE name='000024_conversation_archive.sql'`).Scan(&kept); err != nil || kept != applied {
		t.Fatalf("archive migration reapplied: %q != %q: %v", kept, applied, err)
	}
	entries, err := fs.ReadDir(migrations.Files, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM buddy_schema_migrations`).Scan(&count); err != nil || count != len(entries) {
		t.Fatalf("migration count: %d %v", count, err)
	}
}
