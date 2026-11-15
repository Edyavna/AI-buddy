package migrations_test

import (
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func TestPostgresRecommendationUpgradePreservesReleasedArchive(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	if err := migrations.Apply(ctx, db, migrationsThrough(t, "000024_conversation_archive.sql")); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO conversations(id,user_id,team_id,client_conversation_id,title,archived) VALUES ('recommendation-upgrade','intern_demo_1','team_demo_backend','33333333-3333-4333-8333-333333333333','Архив',true)`); err != nil {
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
	if err := db.QueryRowContext(ctx, `SELECT archived FROM conversations WHERE id='recommendation-upgrade'`).Scan(&archived); err != nil || !archived {
		t.Fatalf("archive changed: %v %v", archived, err)
	}
}
