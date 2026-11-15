package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type unavailableInternContext struct{ SQLStore }

func (unavailableInternContext) SpecialistQuestions(context.Context, string, string, string) ([]ports.SpecialistQuestion, error) {
	return nil, errors.New("intern question dependency unavailable")
}

func TestPostgresMentorChatDoesNotReadInternContext(t *testing.T) {
	f := newFixture(t, fastAgent(), nil, StreamOptions{})
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor})
	service := Service{Store: unavailableInternContext{f.store}, MentorAgent: demo.MentorAgent{}}
	conversation, _, err := service.Create(ctx, uid(501), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(ctx, conversation.ID, uid(502), "Обычный вопрос наставника")
	if err != nil {
		t.Fatalf("mentor chat depends on intern data: %v", err)
	}
	if len(prepared.Input.SpecialistQuestions) != 0 || len(prepared.Input.Requests) != 0 {
		t.Fatalf("intern data in mentor input: %+v", prepared.Input)
	}
}

func TestPostgresMentorChatUsesItsOwnTitleRunner(t *testing.T) {
	f := newFixture(t, fastAgent(), nil, StreamOptions{})
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor})
	service := Service{Store: f.store, Agent: titleAgent{Agent: fastAgent(), title: "Wrong intern title"}, MentorAgent: titleAgent{Agent: demo.MentorAgent{}, title: "Диалог наставника"}}
	conversation, _, err := service.Create(ctx, uid(511), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(ctx, conversation.ID, uid(512), "Вопрос наставника")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Stream(ctx, prepared, newSink(), StreamOptions{}); err != nil {
		t.Fatal(err)
	}
	saved, err := service.Get(ctx, conversation.ID)
	if err != nil || saved.Title != "Диалог наставника" {
		t.Fatalf("wrong role's title runner: %+v %v", saved, err)
	}
}
