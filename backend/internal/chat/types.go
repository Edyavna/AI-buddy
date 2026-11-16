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
	Archived  bool      `json:"archived"`
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	StepID    *string   `json:"step_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ClientID  string    `json:"client_conversation_id"`
	Markdown  bool      `json:"markdown,omitempty"`
}
type Source struct {
	ID         string     `json:"id"`
	DocumentID string     `json:"document_id"`
	Title      string     `json:"title"`
	Excerpt    string     `json:"excerpt"`
	Direction  string     `json:"direction,omitempty"`
	Version    int64      `json:"version,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}
type APIError struct {
	Error httpapi.Error `json:"error"`
}
type Message struct {
	ConversationTitle string                   `json:"-"`
	ID                string                   `json:"id"`
	ClientID          *string                  `json:"client_message_id"`
	Role              string                   `json:"role"`
	Text              string                   `json:"text"`
	Status            string                   `json:"status"`
	Sources           []Source                 `json:"sources"`
	RequestIDs        []string                 `json:"request_ids"`
	Routing           *ports.RoutingSuggestion `json:"routing,omitempty"`
	Outcome           ports.Outcome            `json:"outcome,omitempty"`
	Error             *APIError                `json:"error,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	InReplyTo         *string                  `json:"in_reply_to_message_id"`
}
type Reservation struct {
	Conversation    Conversation
	User, Assistant Message
	Previous        []Message
}
type Store interface {
	List(context.Context, identity.Principal, bool) ([]Conversation, error)
	SetArchived(context.Context, identity.Principal, string, bool) (Conversation, error)
	Create(context.Context, identity.Principal, string, *string, bool) (Conversation, bool, error)
	Get(context.Context, identity.Principal, string) (Conversation, error)
	Messages(context.Context, identity.Principal, string) ([]Message, error)
	Begin(context.Context, identity.Principal, string, string, string, string) (Reservation, error)
	SavePartial(context.Context, identity.Principal, string, string, string) error
	Finish(context.Context, identity.Principal, string, Message) (Message, error)
}
type Service struct {
	Knowledge   ports.Knowledge
	Store       Store
	Agent       ports.Agent
	MentorAgent ports.Agent
	Requests    ports.RequestReader
}

func principal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return p, err
	}
	if p.Role != identity.Intern && p.Role != identity.Mentor {
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
func historyPairs(previous []Message, text string) [][2]Message {
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
	return pairs[start:]
}

func history(previous []Message, text string) []ports.ChatMessage {
	pairs := historyPairs(previous, text)
	out := make([]ports.ChatMessage, 0, 2*len(pairs))
	for _, pair := range pairs {
		for _, m := range pair {
			out = append(out, ports.ChatMessage{Role: m.Role, Text: m.Text})
		}
	}
	return out
}

// Retain the document versions that the model could see via previous answers.
// A follow-up may quote or paraphrase those answers without calling a tool again.
func historySources(previous []Message, text string) []Source {
	var out []Source
	for _, pair := range historyPairs(previous, text) {
		for _, source := range pair[1].Sources {
			source.Excerpt = ""
			out = appendSource(out, source)
		}
	}
	return out
}

func appendSource(sources []Source, next Source) []Source {
	for _, source := range sources {
		if source.DocumentID == next.DocumentID && source.Version == next.Version {
			return sources
		}
	}
	return append(sources, next)
}
