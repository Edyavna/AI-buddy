package app

import (
	"context"
	"errors"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/drafts"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresDraftEditAndLinkRollback(t *testing.T) {
	db := testkit.Postgres(t)
	_, _, conversation := prepareDraft(t, db)
	ctx := testkit.Context(context.Background())
	owner, err := identity.Require(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := accessrequests.Store{DB: db}
	before, err := store.Get(ctx, requestID)
	if err != nil {
		t.Fatal(err)
	}
	chats := chat.SQLStore{DB: db}
	reserved, err := chats.Begin(ctx, owner, conversation,
		"99999999-1111-4444-8888-000000000005", "Уточнение причины", "draft-edit-test")
	if err != nil {
		t.Fatal(err)
	}

	service := drafts.New(store)
	input := before.DraftInput
	input.AssistantMessageID = reserved.Assistant.ID
	input.Reason = "Уточнённая причина"
	updated, err := service.SaveAndLink(ctx, input)
	if err != nil || updated.Version != before.Version+1 || updated.Revision != before.Revision+1 {
		t.Fatalf("edit: %+v %v", updated, err)
	}
	repeat, err := service.SaveAndLink(ctx, input)
	if err != nil || repeat.Version != updated.Version || repeat.Revision != updated.Revision {
		t.Fatalf("repeating an edit must preserve its version: %+v %v", repeat, err)
	}
	input.RequestID = "duplicate-draft"
	if _, err := service.SaveAndLink(ctx, input); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("duplicate draft: %v", err)
	}
	input.RequestID = requestID
	input.AssistantMessageID = before.AssistantMessageID
	input.Reason = "Не должна сохраниться"
	if _, err := service.SaveAndLink(ctx, input); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("link to a completed message: %v", err)
	}
	stored, err := store.Get(ctx, requestID)
	if err != nil || stored.Version != updated.Version || stored.Revision != updated.Revision ||
		stored.Reason != updated.Reason || stored.AssistantMessageID != reserved.Assistant.ID {
		t.Fatalf("failed link must roll back the entire edit: %+v %v", stored, err)
	}
	if _, err := store.Confirm(ctx, requestID, before.Version, nil); !errors.Is(err, accessrequests.ErrDraftChanged) {
		t.Fatalf("stale version after an actual edit: %v", err)
	}
}
