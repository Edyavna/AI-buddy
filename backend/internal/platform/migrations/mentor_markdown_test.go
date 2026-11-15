package migrations_test

import (
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func TestPostgresMarkdownUpgradePreservesReleasedArchive(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	if err := migrations.Apply(ctx, db, migrationsThrough(t, "000026_step_blocking_history.sql")); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO conversations(id,user_id,team_id,client_conversation_id,title,archived) VALUES ('markdown-upgrade','mentor_demo_1','team_demo_backend','75757575-7575-4575-8575-757575757575','Архив наставника',true)`); err != nil {
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
	if err := db.QueryRowContext(ctx, `SELECT archived,markdown FROM conversations WHERE id='markdown-upgrade'`).Scan(&archived, &markdown); err != nil || !archived || markdown {
		t.Fatalf("archive changed or old chat became Markdown: %v %v %v", archived, markdown, err)
	}
}
