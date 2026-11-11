package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func TestPostgresChatOutcome(t *testing.T) {
	f := newFixture(t, agentFunc(func(_ context.Context, in ports.AgentInput, _ ports.Emit) (ports.AgentResult, error) {
		switch in.Text {
		case "gap":
			return ports.AgentResult{Outcome: ports.OutcomeKnowledgeGap}, nil
		case "clarify":
			return ports.AgentResult{Outcome: ports.OutcomeNeedsClarification}, nil
		case "failure":
			return ports.AgentResult{Outcome: ports.OutcomeKnowledgeGap}, ports.ErrUnavailable
		default:
			return ports.AgentResult{}, nil
		}
	}), nil, StreamOptions{})
	conversation := f.create(t, 900)
	path := "/conversations/" + conversation.ID + "/messages"
	for i, tt := range []struct {
		text    string
		outcome ports.Outcome
	}{
		{"gap", ports.OutcomeKnowledgeGap},
		{"clarify", ports.OutcomeNeedsClarification},
		{"legacy", ports.OutcomeUnknown},
		{"failure", ports.OutcomeUnknown},
	} {
		body := fmt.Sprintf(`{"client_message_id":%q,"text":%q}`, uid(901+i), tt.text)
		stream := f.call(t, "POST", path, body, f.cookie, 200).Body.String()
		if !strings.Contains(stream, fmt.Sprintf(`"outcome":%q`, tt.outcome)) {
			t.Fatalf("%s: %s", tt.text, stream)
		}
		messages := decodeMessages(t, f.call(t, "GET", path, "", f.cookie, 200))
		user, assistant := messages[len(messages)-2], messages[len(messages)-1]
		if user.Outcome != "" || assistant.Outcome != tt.outcome {
			t.Fatalf("unexpected persisted outcome: %+v %+v", user, assistant)
		}
	}
}
