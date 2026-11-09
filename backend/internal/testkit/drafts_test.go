package testkit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestDraftCallback(t *testing.T) {
	ctx := testkit.Context(context.Background())
	var unavailable testkit.Drafts
	if _, err := unavailable.SaveAndLink(ctx, ports.DraftInput{}); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := unavailable.SaveAndLink(context.Background(), ports.DraftInput{}); err == nil {
		t.Fatal("accepted missing principal")
	}
	fake := testkit.Drafts(func(ctx context.Context, in ports.DraftInput) (ports.Draft, error) {
		return ports.Draft{DraftInput: in, Version: 1, Revision: 1}, nil
	})
	if got, err := fake.SaveAndLink(ctx, ports.DraftInput{RequestID: "r"}); err != nil || got.RequestID != "r" {
		t.Fatal(err)
	}
}
