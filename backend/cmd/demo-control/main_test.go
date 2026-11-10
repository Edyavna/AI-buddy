package main

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestCommands(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	if err := integrations.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	c, _ := integrations.NewControl(db, true)
	p := integrations.New(db)
	task, err := p.FirstTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"approve-me", "reject-me"} {
		if _, err := p.Submit(ctx, ports.AccessSubmission{RequestID: id, Resource: task.Resource, Reason: "Демо"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"list"}, {"count"}, {"approve", "approve-me"}, {"reject", "reject-me", "Тестовый отказ"}, {"fault", "approve-me", "lookup-unavailable"}, {"fault", "approve-me", "none"}} {
		var output bytes.Buffer
		if err := execute(ctx, c, args, &output); err != nil {
			t.Fatal(args, err)
		}
		if args[0] == "count" && strings.TrimSpace(output.String()) != "2" {
			t.Fatal(output.String())
		}
	}
	for _, args := range [][]string{nil, {"unknown"}, {"count", "extra"}, {"list", "extra"}, {"approve"}, {"reject"}, {"fault"}, {"fault", "x", "bad"}} {
		if err := execute(ctx, c, args, &bytes.Buffer{}); err == nil {
			t.Fatal(args)
		}
	}
	var name string
	if err := db.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	t.Setenv("DATABASE_URL", u.String())
	t.Setenv("DATABASE_ENABLED", "true")
	t.Setenv("DEMO_MODE", "true")
	if err := run([]string{"list"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"approve", "missing"}, {"reject", "approve-me", "Отказ"}, {"fault", "x", "invalid"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatal(args)
		}
	}
}

func TestRequiresDemo(t *testing.T) {
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("DATABASE_ENABLED", "true")
	t.Setenv("DATABASE_URL", "postgres://unused@localhost/unused")
	if err := run([]string{"count"}, &bytes.Buffer{}); err == nil {
		t.Fatal("operator enabled outside demo")
	}
}
