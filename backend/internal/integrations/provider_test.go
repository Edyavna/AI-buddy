package integrations_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresLifecycle(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	if err := integrations.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	p := integrations.New(db)
	c, err := integrations.NewControl(db, true)
	if err != nil {
		t.Fatal(err)
	}
	task, err := p.FirstTask(ctx)
	if err != nil || task.Resource.ID != testkit.ResourceID {
		t.Fatal(task, err)
	}
	input := ports.AccessSubmission{RequestID: "repeat", Resource: task.Resource, Reason: "Учебный запуск"}
	var wg sync.WaitGroup
	results := make(chan ports.AccessResult, 2)
	for range 2 {
		wg.Go(func() {
			r, err := p.Submit(ctx, input)
			if err != nil {
				t.Error(err)
			}
			results <- r
		})
	}
	wg.Wait()
	close(results)
	var number string
	for r := range results {
		if r.Status != "pending" || r.ExternalID == "" {
			t.Fatal(r)
		}
		if number != "" && number != r.ExternalID {
			t.Fatal("duplicate")
		}
		number = r.ExternalID
	}
	if count, err := c.Count(ctx); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	check := ports.AccessCheck{ResourceID: task.Resource.ID, Permission: "read"}
	if granted, err := p.Check(ctx, check); err != nil || granted {
		t.Fatal(granted, err)
	}
	changed := input
	changed.Reason = "Другой payload"
	if _, err := p.Submit(ctx, changed); !errors.Is(err, ports.ErrConflict) {
		t.Fatal(err)
	}
	for _, owner := range []identity.Principal{
		{UserID: "intern_demo_2", TeamID: testkit.TeamID, Role: identity.Intern},
		{UserID: "intern_demo_other", TeamID: "team_demo_frontend", Role: identity.Intern},
	} {
		other := identity.WithPrincipal(context.Background(), owner)
		if _, err := p.Lookup(other, input.RequestID); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := p.Submit(other, input); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal(err)
		}
		task, err := p.FirstTask(other)
		if err != nil || task.ID == "task_demo_1" {
			t.Fatal(task, err)
		}
	}
	for range 2 {
		if err := c.Decide(ctx, input.RequestID, "granted", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Decide(ctx, input.RequestID, "rejected", "Отказ"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal(err)
	}
	if err := integrations.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	restarted := integrations.New(db)
	if granted, err := restarted.Check(ctx, check); err != nil || !granted {
		t.Fatal(granted, err)
	}
	if r, err := restarted.Submit(ctx, input); err != nil || r.ExternalID != number || r.Status != "granted" {
		t.Fatal(r, err)
	}
	if rows, err := c.List(ctx); err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}

func TestPostgresFaultsAndMappings(t *testing.T) {
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
	input := ports.AccessSubmission{RequestID: "fault", Resource: task.Resource, Reason: "Демо"}
	if err := c.SetFault(ctx, input.RequestID, "reject-before-write"); err != nil {
		t.Fatal(err)
	}
	r, err := p.Submit(ctx, input)
	if err != nil || r.Status != "failed" || r.ExternalID != "" || r.FailureReason == "" {
		t.Fatal(r, err)
	}
	if _, err := p.Lookup(ctx, input.RequestID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if err := c.SetFault(ctx, input.RequestID, "timeout-after-commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Submit(ctx, input); !errors.Is(err, ports.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	r, err = p.Lookup(ctx, input.RequestID)
	if err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	again, err := p.Submit(ctx, input)
	if err != nil || again != r {
		t.Fatal(again, err)
	}
	if err := c.SetFault(ctx, input.RequestID, "lookup-unavailable"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Lookup(ctx, input.RequestID); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := c.SetFault(ctx, input.RequestID, "none"); err != nil {
		t.Fatal(err)
	}
	if err := c.Decide(ctx, input.RequestID, "rejected", "Синтетический отказ"); err != nil {
		t.Fatal(err)
	}
	if r, err := p.Lookup(ctx, input.RequestID); err != nil || r.Status != "rejected" || r.FailureReason == "" {
		t.Fatal(r, err)
	}
	if count, err := c.Count(ctx); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// A different external system uses the same internal resource and permission.
	if _, err := db.Exec(`UPDATE demo_external.resources SET external_id='other/system/42',external_role='viewer' WHERE id=$1`, task.Resource.ID); err != nil {
		t.Fatal(err)
	}
	input.RequestID = "mapped"
	if _, err := p.Submit(ctx, input); err != nil {
		t.Fatal(err)
	}
	var resource, role string
	if err := db.QueryRow(`SELECT external_resource_id,external_role FROM demo_external.access_requests WHERE client_reference='mapped'`).Scan(&resource, &role); err != nil || resource != "other/system/42" || role != "viewer" {
		t.Fatal(resource, role, err)
	}
	if err := c.Decide(ctx, input.RequestID, "granted", ""); err != nil {
		t.Fatal(err)
	}
	if granted, err := p.Check(ctx, ports.AccessCheck{ResourceID: task.Resource.ID, Permission: "read"}); err != nil || !granted {
		t.Fatal(granted, err)
	}
}

func TestAuthorizationAndValidation(t *testing.T) {
	p := integrations.New(nil)
	ctx := context.Background()
	if _, err := p.FirstTask(ctx); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := p.Check(ctx, ports.AccessCheck{}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := p.Submit(ctx, ports.AccessSubmission{}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := p.Lookup(ctx, "x"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	ctx = testkit.Context(ctx)
	if _, err := p.Submit(ctx, ports.AccessSubmission{}); !errors.Is(err, ports.ErrInvalid) {
		t.Fatal(err)
	}
	mentor := identity.WithPrincipal(ctx, identity.Principal{UserID: "mentor", TeamID: testkit.TeamID, Role: identity.Mentor})
	if _, err := p.FirstTask(mentor); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.FirstTask(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := integrations.NewControl(nil, false); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal(err)
	}
	if err := integrations.Seed(ctx, nil, false); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal(err)
	}
}
