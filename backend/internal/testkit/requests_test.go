package testkit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestRequestReader(t *testing.T) {
	ctx := testkit.Context(context.Background())
	check := ports.AccessCheck{ResourceID: testkit.ResourceID, Permission: "read"}
	active := ports.RequestState{Draft: ports.Draft{DraftInput: ports.DraftInput{RequestID: "r1", Resource: ports.Resource{ID: check.ResourceID, Permission: check.Permission}}}, Status: "pending"}
	terminal := active
	terminal.RequestID = "r0"
	terminal.Status = "rejected"
	other := active
	other.RequestID = "r2"
	other.Resource.Permission = "write"
	r := testkit.Requests{Items: []ports.RequestState{terminal, other, active}}
	if got, err := r.Get(ctx, "r0"); err != nil || got.Status != "rejected" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := r.FindActive(ctx, check); err != nil || got.RequestID != "r1" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := r.Get(ctx, "missing"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := (testkit.Requests{Items: []ports.RequestState{terminal}}).FindActive(ctx, check); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := r.Get(context.Background(), "r1"); err == nil {
		t.Fatal("untrusted read")
	}
	if _, err := r.FindActive(context.Background(), check); err == nil {
		t.Fatal("untrusted search")
	}
	r.Items = append(r.Items, active)
	if _, err := r.FindActive(ctx, check); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("duplicate active request not detected", err)
	}
}
