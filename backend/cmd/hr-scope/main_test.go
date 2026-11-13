package main

import (
	"net/url"
	"os"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestHRScopeCommand(t *testing.T) {
	if err := run([]string{"unknown"}); err == nil {
		t.Fatal("invalid command accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://buddy@127.0.0.1:1/invalid?sslmode=disable")
	if err := run([]string{"list"}); err == nil {
		t.Fatal("unavailable DB accepted")
	}
	db := testkit.UnmigratedPostgres(t)
	var name string
	if err := db.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	dsn.Path = "/" + name
	t.Setenv("DATABASE_URL", dsn.String())
	if err := run([]string{"list"}); err == nil {
		t.Fatal("unmigrated database accepted")
	}
	if err := migrations.Apply(t.Context(), db, migrations.Files); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list"}, {"revoke", "hr_demo_1", "team_demo_backend"}, {"grant", "hr_demo_1", "team_demo_backend"}} {
		if err := run(args); err != nil {
			t.Fatal(args, err)
		}
	}
	if err := run([]string{"grant", "intern_demo_1", "team_demo_backend"}); err == nil {
		t.Fatal("granted HR access to an intern")
	}
}
