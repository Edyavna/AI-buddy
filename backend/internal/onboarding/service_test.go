package onboarding

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type accessFunc func(context.Context, ports.AccessCheck) (bool, error)

func (f accessFunc) Check(ctx context.Context, c ports.AccessCheck) (bool, error) { return f(ctx, c) }

type memoryStore struct {
	snapshot          Snapshot
	readErr, patchErr error
	beforePatch       func()
	patches           int
}

func (m *memoryStore) Read(context.Context, identity.Principal) (Snapshot, error) {
	s := m.snapshot
	s.Steps = slices.Clone(s.Steps)
	return s, m.readErr
}
func (m *memoryStore) Patch(_ context.Context, _ identity.Principal, revision int64, id, status string) (Snapshot, error) {
	m.patches++
	if m.beforePatch != nil {
		m.beforePatch()
	}
	if m.patchErr != nil {
		return Snapshot{}, m.patchErr
	}
	if revision != m.snapshot.Revision {
		return Snapshot{}, ErrPlanChanged
	}
	for i := range m.snapshot.Steps {
		if m.snapshot.Steps[i].ID == id {
			m.snapshot.Steps[i].Status = status
		}
	}
	m.snapshot.Revision++
	return m.Read(context.Background(), identity.Principal{})
}
func internContext() context.Context {
	return identity.WithPrincipal(context.Background(), identity.Principal{UserID: "u", TeamID: "t", Role: identity.Intern})
}
func fixtureSnapshot() Snapshot {
	return Snapshot{ID: "p", Title: "План", Revision: 1, Steps: []Record{
		{Step: Step{ID: "s", Title: "Первый", Status: "in_progress"}, Requirement: &ports.AccessCheck{ResourceID: "repo", Permission: "read"}},
	}}
}
func TestProjection(t *testing.T) {
	reason := "Ожидается доступ"
	for _, tt := range []struct {
		name     string
		steps    []Record
		progress int
		next     string
	}{
		{"empty", nil, 0, ""},
		{"floor", []Record{{Step: Step{ID: "a", Status: "completed"}}, {Step: Step{ID: "b", Status: "in_progress"}}, {Step: Step{ID: "c", Status: "not_started"}}}, 33, "b"},
		{"completed", []Record{{Step: Step{ID: "a", Status: "completed"}}}, 100, ""},
		{"blocked", []Record{{Step: Step{ID: "a", Status: "blocked", BlockedReason: &reason}}}, 0, ""},
		{"prerequisite", []Record{{Step: Step{ID: "a", Status: "in_progress"}}, {Step: Step{ID: "b", Status: "not_started"}, Prerequisites: []string{"a"}}}, 0, "a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := project(Snapshot{Steps: tt.steps}, nil, identity.Principal{})
			if p.ProgressPercent != tt.progress {
				t.Fatal(p)
			}
			if tt.next == "" {
				if p.NextStepID != nil {
					t.Fatal(p)
				}
			} else if p.NextStepID == nil || *p.NextStepID != tt.next {
				t.Fatal(p)
			}
			if p.Steps == nil {
				t.Fatal("null steps")
			}
			for _, s := range p.Steps {
				if s.RequestIDs == nil || s.AllowedStatuses == nil {
					t.Fatal("null arrays")
				}
				if s.Status == "blocked" && (s.BlockedReason == nil || *s.BlockedReason == "" || len(s.AllowedStatuses) != 0) {
					t.Fatal(s)
				}
			}
		})
	}
}
func TestTransitionsAndAccess(t *testing.T) {
	ctx := internContext()
	store := &memoryStore{snapshot: fixtureSnapshot()}
	granted := false
	checks := 0
	svc := Service{Store: store, Access: accessFunc(func(context.Context, ports.AccessCheck) (bool, error) { checks++; return granted, nil })}
	p, err := svc.Get(ctx)
	if err != nil || slices.Contains(p.Steps[0].AllowedStatuses, "completed") {
		t.Fatal(p, err)
	}
	if _, err := svc.Update(ctx, "s", "completed"); !errors.Is(err, ErrTransition) || store.patches != 0 {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, "s", "not_started"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, "s", "not_started"); err != nil {
		t.Fatal("idempotent PATCH failed", err)
	}
	granted = true
	p, err = svc.Update(ctx, "s", "completed")
	if err != nil || p.ProgressPercent != 100 {
		t.Fatal(p, err)
	}
	if checks < 4 {
		t.Fatal("access not rechecked")
	}
	for _, tt := range []struct {
		id, status string
		want       error
	}{
		{"foreign", "completed", ports.ErrNotFound}, {"s", "blocked", ports.ErrInvalid}, {"s", "", ports.ErrInvalid},
	} {
		if _, err := svc.Update(ctx, tt.id, tt.status); !errors.Is(err, tt.want) {
			t.Fatal(tt, err)
		}
	}
	svc.Access = nil
	if _, err := svc.Get(ctx); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	svc.Access = accessFunc(func(context.Context, ports.AccessCheck) (bool, error) { return false, ports.ErrUnavailable })
	if _, err := svc.Update(ctx, "s", "in_progress"); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
}
func TestStaleButtonCannotBypassNewBlock(t *testing.T) {
	ctx := internContext()
	store := &memoryStore{snapshot: fixtureSnapshot()}
	svc := Service{Store: store, Access: accessFunc(func(context.Context, ports.AccessCheck) (bool, error) { return true, nil })}
	store.beforePatch = func() {
		reason := "Подтверждена заявка"
		store.snapshot.Steps[0].Status = "blocked"
		store.snapshot.Steps[0].BlockedReason = &reason
		store.snapshot.Revision++
		store.beforePatch = nil
	}
	if _, err := svc.Update(ctx, "s", "completed"); !errors.Is(err, ErrTransition) {
		t.Fatal(err)
	}
	if store.snapshot.Steps[0].Status != "blocked" {
		t.Fatal("stale PATCH overwrote block")
	}
	// Even a repeated underlying user status cannot escape an effective block.
	if _, err := svc.Update(ctx, "s", "in_progress"); !errors.Is(err, ErrTransition) {
		t.Fatal(err)
	}
	store.snapshot = fixtureSnapshot()
	store.patchErr = ErrPlanChanged
	if _, err := svc.Update(ctx, "s", "completed"); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
}
func TestServiceAuthorizationAndErrors(t *testing.T) {
	store := &memoryStore{snapshot: fixtureSnapshot()}
	svc := Service{Store: store}
	for _, ctx := range []context.Context{context.Background(), identity.WithPrincipal(context.Background(), identity.Principal{UserID: "m", TeamID: "t", Role: identity.Mentor})} {
		if _, err := svc.Get(ctx); err == nil {
			t.Fatal("unauthorized GET")
		}
		if _, err := svc.Update(ctx, "s", "completed"); err == nil {
			t.Fatal("unauthorized PATCH")
		}
	}
	store.readErr = ports.ErrUnavailable
	if _, err := svc.Get(internContext()); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := svc.Update(internContext(), "s", "in_progress"); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	store.readErr = nil
	store.patchErr = ports.ErrUnavailable
	svc.Access = accessFunc(func(context.Context, ports.AccessCheck) (bool, error) { return true, nil })
	if _, err := svc.Update(internContext(), "s", "completed"); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	// One access call per resource/permission, even when several steps need it.
	store.snapshot.Steps = append(store.snapshot.Steps, store.snapshot.Steps[0])
	calls := 0
	svc.Access = accessFunc(func(context.Context, ports.AccessCheck) (bool, error) { calls++; return true, nil })
	if _, err := svc.Get(internContext()); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
