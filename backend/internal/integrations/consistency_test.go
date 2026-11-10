package integrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestApprovalRollbackAndUnavailableDatabase(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	if err := integrations.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	p := integrations.New(db)
	c, _ := integrations.NewControl(db, true)
	task, err := p.FirstTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input := ports.AccessSubmission{RequestID: "rollback", Resource: task.Resource, Reason: "Демо"}
	if _, err := p.Submit(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE demo_external.access_requests ADD CONSTRAINT test_decision_failure CHECK(status <> 'granted')`); err != nil {
		t.Fatal(err)
	}
	if err := c.Decide(ctx, input.RequestID, "granted", ""); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if granted, err := p.Check(ctx, ports.AccessCheck{ResourceID: task.Resource.ID, Permission: "read"}); err != nil || granted {
		t.Fatal("grant escaped rollback", granted, err)
	}
	if r, err := p.Lookup(ctx, input.RequestID); err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	if _, err := p.Check(ctx, ports.AccessCheck{ResourceID: "repo_demo_frontend", Permission: "read"}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if err := c.Decide(ctx, input.RequestID, "invalid", ""); !errors.Is(err, ports.ErrInvalid) {
		t.Fatal(err)
	}
	if err := c.Decide(ctx, input.RequestID, "rejected", ""); !errors.Is(err, ports.ErrInvalid) {
		t.Fatal(err)
	}
	if err := c.SetFault(ctx, "", "none"); !errors.Is(err, ports.ErrInvalid) {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Lookup(ctx, input.RequestID); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := p.FirstTask(ctx); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := p.Submit(ctx, input); !errors.Is(err, ports.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if _, err := c.List(ctx); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
}
