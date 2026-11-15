package migrations_test

import (
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func TestPostgresMemoryUpgradePreservesMarkdownAndArchive(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	if err := migrations.Apply(ctx, db, migrationsThrough(t, "000027_mentor_markdown.sql")); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO conversations(id,user_id,team_id,client_conversation_id,title,archived,markdown) VALUES ('memory-upgrade','mentor_demo_1','team_demo_backend','82828282-8282-4282-8282-828282828282','Инструкция',true,true)`); err != nil {
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
	var archived, markdown bool
	if err := db.QueryRowContext(ctx, `SELECT archived,markdown FROM conversations WHERE id='memory-upgrade'`).Scan(&archived, &markdown); err != nil || !archived || !markdown {
		t.Fatal("released conversation changed", archived, markdown, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM memory_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatal("upgrade created memory without confirmation", count, err)
	}
}
