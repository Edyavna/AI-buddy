package testkit_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestAgentAndKnowledge(t *testing.T) {
	ctx := testkit.Context(context.Background())
	agent := testkit.Agent{Events: []ports.AgentEvent{{Kind: "answer.delta", Text: "hello"}}}
	var answer string
	if _, err := agent.Run(ctx, ports.AgentInput{}, func(ctx context.Context, event ports.AgentEvent) error { answer += event.Text; return nil }); err != nil || answer != "hello" {
		t.Fatal(err)
	}
	sentinel := errors.New("persist failed")
	if _, err := agent.Run(ctx, ports.AgentInput{}, func(context.Context, ports.AgentEvent) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := agent.Run(canceled, ports.AgentInput{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	between, stop := context.WithCancel(ctx)
	twoEvents := testkit.Agent{Events: []ports.AgentEvent{{Kind: "answer.delta"}, {Kind: "answer.delta"}}}
	_, err := twoEvents.Run(between, ports.AgentInput{}, func(context.Context, ports.AgentEvent) error { stop(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	knowledge := testkit.Knowledge{}
	sources, err := knowledge.Search(ctx, "setup")
	if err != nil || len(sources) != 1 {
		t.Fatal(err)
	}
	if sources, err := knowledge.Search(ctx, ""); err != nil || len(sources) != 0 {
		t.Fatal(err)
	}
	if doc, err := knowledge.Read(ctx, testkit.DocumentID); err != nil || doc.ID != sources[0].DocumentID {
		t.Fatal(err)
	}
	if _, err := knowledge.Read(ctx, "foreign"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if task, err := (testkit.Tasks{}).FirstTask(ctx); err != nil || task.Resource.ID != testkit.ResourceID {
		t.Fatal(err)
	}
	for _, invalid := range []context.Context{context.Background(), identity.WithPrincipal(ctx, identity.Principal{UserID: "foreign", TeamID: testkit.TeamID, Role: identity.Intern}), identity.WithPrincipal(ctx, identity.Principal{UserID: testkit.UserID, TeamID: testkit.TeamID, Role: identity.Mentor})} {
		if _, err := knowledge.Search(invalid, "x"); err == nil {
			t.Fatal("leaked search")
		}
		if _, err := knowledge.Read(invalid, testkit.DocumentID); err == nil {
			t.Fatal("leaked document")
		}
		if _, err := (testkit.Tasks{}).FirstTask(invalid); err == nil {
			t.Fatal("leaked task")
		}
		if _, err := agent.Run(invalid, ports.AgentInput{}, nil); err == nil {
			t.Fatal("agent accepted untrusted input")
		}
	}
}

func TestAccessIdempotency(t *testing.T) {
	ctx := testkit.Context(context.Background())
	a := &testkit.Access{}
	in := ports.AccessSubmission{RequestID: "request_demo_1", Resource: ports.Resource{ID: testkit.ResourceID, Name: "Demo", Permission: "read"}, Reason: "setup"}
	if _, err := a.Lookup(ctx, in.RequestID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			result, err := a.Submit(ctx, in)
			if err != nil || result.Status != "pending" || result.ExternalID != "FAKE-request_demo_1" {
				t.Errorf("%+v %v", result, err)
			}
		})
	}
	wg.Wait()
	if result, err := a.Lookup(ctx, in.RequestID); err != nil || result.ExternalID != "FAKE-request_demo_1" {
		t.Fatal(err)
	}
	in.Reason = "different"
	if _, err := a.Submit(ctx, in); !errors.Is(err, ports.ErrConflict) {
		t.Fatal(err)
	}
	in.RequestID = ""
	if _, err := a.Submit(ctx, in); !errors.Is(err, ports.ErrInvalid) {
		t.Fatal(err)
	}
	in.RequestID = "r2"
	in.Resource.ID = "foreign"
	if _, err := a.Submit(ctx, in); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if granted, err := a.Check(ctx, ports.AccessCheck{ResourceID: testkit.ResourceID, Permission: "read"}); err != nil || granted {
		t.Fatal(err)
	}
	if _, err := a.Check(ctx, ports.AccessCheck{ResourceID: testkit.ResourceID, Permission: "admin"}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := a.Check(context.Background(), ports.AccessCheck{}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := a.Submit(context.Background(), in); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := a.Lookup(context.Background(), "r"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}
