package integrations_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestProcessRestart(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	if err := integrations.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
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
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var previous ports.AccessResult
	for range 2 {
		cmd := exec.Command(binary, "-test.run=^TestRestartProbe$")
		cmd.Env = append(os.Environ(), "ISSUE13_PROBE_DSN="+u.String())
		data, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var result ports.AccessResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(string(data), err)
		}
		if previous.ExternalID != "" && result != previous {
			t.Fatal(previous, result)
		}
		previous = result
	}
	c, _ := integrations.NewControl(db, true)
	if count, err := c.Count(ctx); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestRestartProbe(t *testing.T) {
	dsn := os.Getenv("ISSUE13_PROBE_DSN")
	if dsn == "" {
		return
	}
	ctx := testkit.Context(context.Background())
	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	p := integrations.New(db)
	task, err := p.FirstTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input := ports.AccessSubmission{RequestID: "process-restart", Resource: task.Resource, Reason: "Демо после перезапуска"}
	if _, err := p.Submit(ctx, input); err != nil {
		t.Fatal(err)
	}
	c, _ := integrations.NewControl(db, true)
	if err := c.Decide(ctx, input.RequestID, "granted", ""); err != nil {
		t.Fatal(err)
	}
	result, err := p.Lookup(ctx, input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if granted, err := p.Check(ctx, ports.AccessCheck{ResourceID: task.Resource.ID, Permission: "read"}); err != nil || !granted {
		t.Fatal(granted, err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		t.Fatal(err)
	}
	db.Close()
	os.Exit(0)
}
