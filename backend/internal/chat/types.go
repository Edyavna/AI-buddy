// Package chat owns durable conversations and the Agent-to-SSE boundary.
package chat

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var (
	ErrDuplicate = errors.New("message already exists")
	ErrBusy      = errors.New("generation in progress")
)

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	StepID    *string   `json:"step_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ClientID  string    `json:"client_conversation_id"`
}
type Source struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
	Excerpt    string `json:"excerpt"`
}
type APIError struct {
	Error httpapi.Error `json:"error"`
}
type Message struct {
	ID         string        `json:"id"`
	ClientID   *string       `json:"client_message_id"`
	Role       string        `json:"role"`
	Text       string        `json:"text"`
	Status     string        `json:"status"`
	Sources    []Source      `json:"sources"`
	RequestIDs []string      `json:"request_ids"`
	Outcome    ports.Outcome `json:"outcome,omitempty"`
	Error      *APIError     `json:"error,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	InReplyTo  *string       `json:"in_reply_to_message_id"`
}
type Reservation struct {
	Conversation    Conversation
	User, Assistant Message
	Previous        []Message
}
type Store interface {
	List(context.Context, identity.Principal) ([]Conversation, error)
	Create(context.Context, identity.Principal, string, *string) (Conversation, bool, error)
	Get(context.Context, identity.Principal, string) (Conversation, error)
	Messages(context.Context, identity.Principal, string) ([]Message, error)
	Begin(context.Context, identity.Principal, string, string, string, string) (Reservation, error)
	SavePartial(context.Context, identity.Principal, string, string, string) error
	Finish(context.Context, identity.Principal, string, Message) (Message, error)
}
type Service struct {
	Store    Store
	Agent    ports.Agent
	Requests ports.RequestReader
}

func principal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return p, err
	}
	if p.Role != identity.Intern {
		return p, ports.ErrForbidden
	}
	return p, nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validUUID(id string) bool { return uuidPattern.MatchString(id) }
func validText(text string) bool {
	return strings.TrimSpace(text) != "" && utf8.RuneCountInString(text) <= 8000
}

// History includes only complete pairs; the current question is passed in Text.
// Keep newest whole pairs, reserving both a message and code points for Text.
func history(previous []Message, text string) []ports.ChatMessage {
	pairs := make([][2]Message, 0)
	for i := 1; i < len(previous); i++ {
		u, a := previous[i-1], previous[i]
		if u.Role == "user" && a.Role == "assistant" && a.Status == "completed" && a.InReplyTo != nil && *a.InReplyTo == u.ID {
			pairs = append(pairs, [2]Message{u, a})
		}
	}
	remaining := 24000 - utf8.RuneCountInString(text)
	start := len(pairs)
	count := 0
	for i := len(pairs) - 1; i >= 0; i-- {
		size := utf8.RuneCountInString(pairs[i][0].Text) + utf8.RuneCountInString(pairs[i][1].Text)
		if count+2 > 19 || size > remaining {
			break
		}
		remaining -= size
		count += 2
		start = i
	}
	out := make([]ports.ChatMessage, 0, count)
	for _, pair := range pairs[start:] {
		for _, m := range pair {
			out = append(out, ports.ChatMessage{Role: m.Role, Text: m.Text})
		}
	}
	return out
}
