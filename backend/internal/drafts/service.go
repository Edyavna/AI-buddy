package drafts

import (
	"context"
	"fmt"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// Tx must check ownership of conversation, assistant and step against Principal.
// UpsertDraft must lock/check draft state, bump versions only for changed params,
// reject submitted records, and never perform an external call.
type Tx interface {
	UpsertDraft(context.Context, identity.Principal, ports.DraftInput) (ports.Draft, error)
	LinkAssistant(context.Context, identity.Principal, string, string) error
}
type Store interface {
	WithinTx(context.Context, func(Tx) error) error
}
type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

func (s *Service) SaveAndLink(ctx context.Context, in ports.DraftInput) (ports.Draft, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return ports.Draft{}, err
	}
	if p.Role != identity.Intern {
		return ports.Draft{}, ports.ErrForbidden
	}
	if in.RequestID == "" || in.ConversationID == "" || in.AssistantMessageID == "" || in.Resource.ID == "" || strings.TrimSpace(in.Resource.Name) == "" || strings.TrimSpace(in.Reason) == "" || (in.Resource.Permission != "read" && in.Resource.Permission != "write") || (in.StepID != nil && *in.StepID == "") {
		return ports.Draft{}, ports.ErrInvalid
	}
	var saved ports.Draft
	err = s.store.WithinTx(ctx, func(tx Tx) error {
		var err error
		saved, err = tx.UpsertDraft(ctx, p, in)
		if err != nil {
			return err
		}
		return tx.LinkAssistant(ctx, p, in.AssistantMessageID, saved.RequestID)
	})
	if err != nil {
		return ports.Draft{}, fmt.Errorf("save draft and assistant link: %w", err)
	}
	return saved, nil
}

var _ ports.DraftService = (*Service)(nil)
