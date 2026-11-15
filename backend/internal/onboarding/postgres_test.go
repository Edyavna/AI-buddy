package onboarding_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type accessFunc func(context.Context, ports.AccessCheck) (bool, error)

func (f accessFunc) Check(ctx context.Context, check ports.AccessCheck) (bool, error) {
	return f(ctx, check)
}

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
	store := onboarding.SQLStore{DB: db}
	// One connection proves the provider call does not run inside a transaction.
	db.SetMaxOpenConns(1)
	svc := onboarding.Service{Store: store, Access: accessFunc(func(ctx context.Context, _ ports.AccessCheck) (bool, error) { return true, db.PingContext(ctx) })}
	apply := func(id, status, resource string) error {
		return database.WithinTx(ctx, db, func(tx *sql.Tx) error {
			if err := onboarding.LockPlanTx(ctx, tx, owner()); err != nil {
				return err
			}
			return onboarding.ApplyRequestStateTx(ctx, tx, owner(), "step_local_setup", requestState(id, status, resource))
		})
	}
	rollback := errors.New("request/job write failed")
	err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		if err := onboarding.ApplyRequestStateTx(ctx, tx, owner(), "step_local_setup", requestState("r", "submitting", testkit.ResourceID)); err != nil {
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
	if p.Steps[0].BlockedAt == nil || p.Recommendation.WaitingStep == nil || p.Recommendation.WaitingStep.BlockedAt == nil {
		t.Fatalf("expected blocked_at to be populated, got %+v", p.Steps[0])
	}
	var histCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM onboarding_step_blocking_history WHERE step_id='step_local_setup' AND ended_at IS NULL`).Scan(&histCount); err != nil || histCount == 0 {
		t.Fatalf("expected active blocking history row, got %d %v", histCount, err)
	}
	if _, err := svc.Update(ctx, "step_local_setup", "completed"); !errors.Is(err, onboarding.ErrTransition) {
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
	if p.Steps[0].BlockedAt != nil {
		t.Fatalf("expected blocked_at to be cleared, got %v", p.Steps[0].BlockedAt)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM onboarding_step_blocking_history WHERE step_id='step_local_setup' AND ended_at IS NOT NULL`).Scan(&histCount); err != nil || histCount == 0 {
		t.Fatalf("expected ended blocking history row, got %d %v", histCount, err)
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
	if err != nil || p.ProgressPercent != 5 {
		t.Fatal(p, err)
	}
	// A duplicate grant must never reset completed progress.
	if err := apply("r3", "granted", "other_repo"); err != nil {
		t.Fatal(err)
	}
	p, err = svc.Get(ctx)
	if err != nil || p.ProgressPercent != 5 {
		t.Fatal(p, err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	recovered := onboarding.Service{Store: onboarding.SQLStore{DB: db}, Access: svc.Access}
	p, err = recovered.Get(ctx)
	if err != nil || p.ProgressPercent != 5 || len(p.Steps[0].RequestIDs) != 4 {
		t.Fatal("state lost", p, err)
	}
	// Both ownership dimensions are checked, even for direct Go calls.
	stranger := owner()
	stranger.UserID = "intern_demo_2"
	err = database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		return onboarding.ApplyRequestStateTx(ctx, tx, stranger, "step_local_setup", requestState("x", "pending", testkit.ResourceID))
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	stranger = owner()
	stranger.TeamID = "team_demo_frontend"
	if _, err := store.Read(ctx, stranger); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := store.Patch(ctx, owner(), before.Revision, "step_local_setup", "not_started"); !errors.Is(err, onboarding.ErrPlanChanged) {
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
	svc := onboarding.Service{Store: onboarding.SQLStore{DB: db}, Access: accessFunc(func(context.Context, ports.AccessCheck) (bool, error) {
		once.Do(func() { close(checked); <-release })
		return true, nil
	})}
	result := make(chan error, 1)
	go func() { _, err := svc.Update(ctx, "step_local_setup", "completed"); result <- err }()
	<-checked
	err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		return onboarding.ApplyRequestStateTx(ctx, tx, owner(), "step_local_setup", requestState("r", "submitting", testkit.ResourceID))
	})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, onboarding.ErrTransition) {
		t.Fatal("stale completion accepted", err)
	}
	snapshot, err := (onboarding.SQLStore{DB: db}).Read(ctx, owner())
	if err != nil || snapshot.Steps[0].Status != "blocked" {
		t.Fatal(snapshot, err)
	}
}

func TestPostgresCheckpointAccessRace(t *testing.T) {
	db := testkit.Postgres(t)
	ctx, cancel := context.WithTimeout(testkit.Context(t.Context()), 10*time.Second)
	defer cancel()
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	const stepID = "step_local_setup"
	store := onboarding.SQLStore{DB: db}
	db.SetMaxOpenConns(1)
	checked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	service := onboarding.Checkpoints{DB: db, Access: accessFunc(func(ctx context.Context, _ ports.AccessCheck) (bool, error) {
		if err := db.PingContext(ctx); err != nil {
			return false, err
		}
		once.Do(func() { close(checked); <-release })
		return true, nil
	})}
	apply := func(id, status string) {
		t.Helper()
		if err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
			return onboarding.ApplyRequestStateTx(ctx, tx, owner(), stepID, requestState(id, status, testkit.ResourceID))
		}); err != nil {
			t.Fatal(err)
		}
	}
	read := func() onboarding.Snapshot {
		t.Helper()
		snapshot, err := store.Read(ctx, owner())
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	initial := read()
	result := make(chan error, 1)
	go func() {
		_, err := service.Set(ctx, identity.Intern, "", stepID, onboarding.SetConfirmation{Confirmed: true, Revision: initial.Revision})
		result <- err
	}()
	select {
	case <-checked:
	case err := <-result:
		t.Fatalf("confirmation failed before the grant check: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	func() {
		defer close(release)
		apply("access1", "pending")
	}()
	if err := <-result; !errors.Is(err, onboarding.ErrPlanChanged) {
		t.Fatal("stale access allowed confirmation", err)
	}
	blocked := read()
	if blocked.Steps[0].Status != "blocked" || len(blocked.Steps[0].Confirmations) != 0 {
		t.Fatal(blocked)
	}
	apply("access1", "pending")
	if repeated := read(); repeated.Revision != blocked.Revision {
		t.Fatal("duplicate request state advanced the revision", repeated)
	}
	if _, err := service.Set(ctx, identity.Intern, "", stepID, onboarding.SetConfirmation{Confirmed: true, Revision: blocked.Revision}); !errors.Is(err, onboarding.ErrTransition) {
		t.Fatal(err)
	}
	apply("access1", "granted")
	plan, err := service.Set(ctx, identity.Intern, "", stepID, onboarding.SetConfirmation{Confirmed: true, Revision: read().Revision})
	if err != nil || plan.ProgressPercent != 5 {
		t.Fatal(plan, err)
	}
	apply("access2", "pending")
	held := read()
	if held.Steps[0].Status != "blocked" || len(held.Steps[0].Confirmations) != 1 {
		t.Fatal(held)
	}
	apply("access2", "granted")
	if recovered := read(); recovered.Steps[0].Status != "completed" {
		t.Fatal(recovered)
	}
	apply("access3", "pending")
	plan, err = service.Set(ctx, identity.Intern, "", stepID, onboarding.SetConfirmation{Revision: read().Revision})
	if err != nil || plan.Steps[0].Status != "blocked" || len(plan.Steps[0].Confirmations) != 0 {
		t.Fatal(plan, err)
	}
	apply("access3", "granted")
	if recovered := read(); recovered.Steps[0].Status != "in_progress" {
		t.Fatal(recovered)
	}
}
