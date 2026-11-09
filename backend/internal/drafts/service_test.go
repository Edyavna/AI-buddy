package drafts_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/drafts"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

// This double stages both writes; a LinkAssistant failure must discard the draft.
type store struct {
	committed        tx
	linkErr, saveErr error
	commits          int
}
type tx struct {
	draft            ports.Draft
	link             string
	linkErr, saveErr error
}

func (s *store) WithinTx(ctx context.Context, fn func(drafts.Tx) error) error {
	staged := s.committed
	staged.linkErr = s.linkErr
	staged.saveErr = s.saveErr
	if err := fn(&staged); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.committed = staged
	s.commits++
	return nil
}
func (t *tx) UpsertDraft(ctx context.Context, p identity.Principal, in ports.DraftInput) (ports.Draft, error) {
	if p.UserID != testkit.UserID || p.TeamID != testkit.TeamID || in.ConversationID != "conversation_demo_1" || in.AssistantMessageID != "msg_a1" {
		return ports.Draft{}, ports.ErrNotFound
	}
	if t.saveErr != nil {
		return ports.Draft{}, t.saveErr
	}
	t.draft = ports.Draft{DraftInput: in, Version: 1, Revision: 1}
	return t.draft, nil
}
func (t *tx) LinkAssistant(ctx context.Context, p identity.Principal, messageID, requestID string) error {
	if t.linkErr != nil {
		return t.linkErr
	}
	t.link = messageID + ":" + requestID
	return nil
}
func input() ports.DraftInput {
	return ports.DraftInput{RequestID: "request_demo_1", ConversationID: "conversation_demo_1", AssistantMessageID: "msg_a1", Resource: ports.Resource{ID: testkit.ResourceID, Name: "Тестовый репозиторий", Permission: "read"}, Reason: "Подготовить окружение"}
}
func TestSaveAndLink(t *testing.T) {
	s := &store{}
	service := drafts.New(s)
	ctx := testkit.Context(context.Background())
	saved, err := service.SaveAndLink(ctx, input())
	if err != nil || saved.RequestID != "request_demo_1" || s.committed.link != "msg_a1:request_demo_1" || s.commits != 1 {
		t.Fatalf("%+v %v", saved, err)
	}
	for _, failure := range []string{"save", "link"} {
		t.Run(failure, func(t *testing.T) {
			s := &store{}
			sentinel := errors.New("repository failed")
			if failure == "save" {
				s.saveErr = sentinel
			} else {
				s.linkErr = sentinel
			}
			saved, err := drafts.New(s).SaveAndLink(ctx, input())
			if !errors.Is(err, sentinel) || s.commits != 0 || s.committed.draft.RequestID != "" || saved.RequestID != "" {
				t.Fatalf("partial commit: %+v %v", s, err)
			}
		})
	}
}
func TestSaveRequiresTrustedIdentityAndOwnedMessage(t *testing.T) {
	for _, tt := range []struct {
		ctx  context.Context
		want error
	}{
		{context.Background(), identity.ErrUnauthenticated},
		{identity.WithPrincipal(context.Background(), identity.Principal{UserID: testkit.UserID, TeamID: testkit.TeamID, Role: identity.Mentor}), ports.ErrForbidden},
		{identity.WithPrincipal(context.Background(), identity.Principal{UserID: "other", TeamID: testkit.TeamID, Role: identity.Intern}), ports.ErrNotFound},
	} {
		s := &store{}
		_, err := drafts.New(s).SaveAndLink(tt.ctx, input())
		if !errors.Is(err, tt.want) || s.commits != 0 {
			t.Fatal(err)
		}
	}
	s := &store{}
	in := input()
	in.AssistantMessageID = "foreign"
	if _, err := drafts.New(s).SaveAndLink(testkit.Context(context.Background()), in); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestSaveValidation(t *testing.T) {
	mutations := []func(*ports.DraftInput){
		func(in *ports.DraftInput) { in.RequestID = "" }, func(in *ports.DraftInput) { in.ConversationID = "" },
		func(in *ports.DraftInput) { in.AssistantMessageID = "" }, func(in *ports.DraftInput) { in.Resource.ID = "" },
		func(in *ports.DraftInput) { in.Resource.Name = " " }, func(in *ports.DraftInput) { in.Resource.Permission = "admin" },
		func(in *ports.DraftInput) { in.Reason = " " }, func(in *ports.DraftInput) { empty := ""; in.StepID = &empty },
	}
	for _, mutate := range mutations {
		s := &store{}
		in := input()
		mutate(&in)
		if _, err := drafts.New(s).SaveAndLink(testkit.Context(context.Background()), in); !errors.Is(err, ports.ErrInvalid) || s.commits != 0 {
			t.Fatal(err)
		}
	}
}
