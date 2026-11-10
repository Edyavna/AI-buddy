package onboarding

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func requestState(id, status, resource string) ports.RequestState {
	return ports.RequestState{Draft: ports.Draft{DraftInput: ports.DraftInput{RequestID: id, Resource: ports.Resource{ID: resource, Permission: "read"}}}, Status: status}
}
func owner() identity.Principal {
	return identity.Principal{UserID: testkit.UserID, TeamID: testkit.TeamID, Role: identity.Intern}
}
func TestPostgresPlanTransactionsAndRecovery(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	store := SQLStore{DB: db}
	// One connection proves the provider call does not run inside a transaction.
	db.SetMaxOpenConns(1)
	svc := Service{Store: store, Access: accessFunc(func(ctx context.Context, _ ports.AccessCheck) (bool, error) { return true, db.PingContext(ctx) })}
	apply := func(id, status, resource string) error {
		return database.WithinTx(ctx, db, func(tx *sql.Tx) error {
			if err := LockPlanTx(ctx, tx, owner()); err != nil {
				return err
			}
			return ApplyRequestStateTx(ctx, tx, owner(), "step_local_setup", requestState(id, status, resource))
		})
	}
	rollback := errors.New("request/job write failed")
	err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		if err := ApplyRequestStateTx(ctx, tx, owner(), "step_local_setup", requestState("r", "submitting", testkit.ResourceID)); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	before, err := store.Read(ctx, owner())
	if err != nil {
		t.Fatal(err)
	}
	if before.Steps[0].Status != "in_progress" || len(before.Steps[0].RequestIDs) != 0 {
		t.Fatal("partial commit", before)
	}
	if err := apply("r", "draft", testkit.ResourceID); err != nil {
		t.Fatal(err)
	}
	if err := apply("r", "submitting", testkit.ResourceID); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Get(ctx)
	if err != nil || p.Steps[0].Status != "blocked" || p.Steps[0].BlockedReason == nil {
		t.Fatal(p, err)
	}
	if _, err := svc.Update(ctx, "step_local_setup", "completed"); !errors.Is(err, ErrTransition) {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "rejected", "failed"} {
		if err := apply("r", status, testkit.ResourceID); err != nil {
			t.Fatal(err)
		}
		p, err = svc.Get(ctx)
		if err != nil || p.Steps[0].Status != "blocked" {
			t.Fatal(p, err)
		}
	}
	if err := apply("r2", "granted", testkit.ResourceID); err != nil {
		t.Fatal(err)
	}
	p, err = svc.Get(ctx)
	if err != nil || p.Steps[0].Status != "in_progress" || p.ProgressPercent != 0 {
		t.Fatal("grant did not unblock failed previous attempt", p, err)
	}
	// A distinct required resource remains blocking, even if another was granted.
	if err := apply("r3", "pending", "other_repo"); err != nil {
		t.Fatal(err)
	}
	if err := apply("r2", "granted", testkit.ResourceID); err != nil {
		t.Fatal(err)
	}
	p, err = svc.Get(ctx)
	if err != nil || p.Steps[0].Status != "blocked" {
		t.Fatal(p, err)
	}
	if err := apply("r3", "granted", "other_repo"); err != nil {
		t.Fatal(err)
	}
	// A pending old attempt can finish after another attempt grants the same
	// resource; that late failure must not leave the step permanently blocked.
	if err := apply("r4", "pending", testkit.ResourceID); err != nil {
		t.Fatal(err)
	}
	if err := apply("r4", "failed", testkit.ResourceID); err != nil {
		t.Fatal(err)
	}
	p, err = svc.Get(ctx)
	if err != nil || p.Steps[0].Status != "in_progress" {
		t.Fatal(p, err)
	}
	p, err = svc.Update(ctx, "step_local_setup", "completed")
	if err != nil || p.ProgressPercent != 100 {
		t.Fatal(p, err)
	}
	// A duplicate grant must never reset completed progress.
	if err := apply("r3", "granted", "other_repo"); err != nil {
		t.Fatal(err)
	}
	p, err = svc.Get(ctx)
	if err != nil || p.ProgressPercent != 100 {
		t.Fatal(p, err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	recovered := Service{Store: SQLStore{DB: db}, Access: svc.Access}
	p, err = recovered.Get(ctx)
	if err != nil || p.ProgressPercent != 100 || len(p.Steps[0].RequestIDs) != 4 {
		t.Fatal("state lost", p, err)
	}
	// Both ownership dimensions are checked, even for direct Go calls.
	stranger := owner()
	stranger.UserID = "intern_demo_2"
	err = database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		return ApplyRequestStateTx(ctx, tx, stranger, "step_local_setup", requestState("x", "pending", testkit.ResourceID))
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	stranger = owner()
	stranger.TeamID = "team_demo_frontend"
	if _, err := store.Read(ctx, stranger); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := store.Patch(ctx, owner(), before.Revision, "step_local_setup", "not_started"); !errors.Is(err, ErrPlanChanged) {
		t.Fatal(err)
	}
}
func TestPostgresConcurrentConfirmBlocksStalePatch(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	checked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	svc := Service{Store: SQLStore{DB: db}, Access: accessFunc(func(context.Context, ports.AccessCheck) (bool, error) {
		once.Do(func() { close(checked); <-release })
		return true, nil
	})}
	result := make(chan error, 1)
	go func() { _, err := svc.Update(ctx, "step_local_setup", "completed"); result <- err }()
	<-checked
	err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		return ApplyRequestStateTx(ctx, tx, owner(), "step_local_setup", requestState("r", "submitting", testkit.ResourceID))
	})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrTransition) {
		t.Fatal("stale completion accepted", err)
	}
	snapshot, err := (SQLStore{DB: db}).Read(ctx, owner())
	if err != nil || snapshot.Steps[0].Status != "blocked" {
		t.Fatal(snapshot, err)
	}
}
