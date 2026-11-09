// Package ports defines internal Go seams. These are not HTTP/SSE DTOs.
package ports

import (
	"context"
	"errors"
)

var (
	ErrNotFound       = errors.New("not found or inaccessible")
	ErrForbidden      = errors.New("operation forbidden")
	ErrInvalid        = errors.New("invalid input")
	ErrConflict       = errors.New("idempotency conflict")
	ErrUnavailable    = errors.New("dependency unavailable")
	ErrOutcomeUnknown = errors.New("external outcome unknown; reconcile using the same request ID")
)

type Resource struct{ ID, Name, Permission string }
type Source struct{ ID, DocumentID, Title, Excerpt string }
type Document struct{ ID, Title, Content string }
type Task struct {
	ID, Title, Description string
	Resource               Resource
}
type ChatMessage struct{ Role, Text string }
type AgentInput struct {
	ConversationID, AssistantMessageID, Text string
	StepID                                   *string
	History                                  []ChatMessage
}
type AgentResult struct{ Sources []Source }
type ToolEvent struct{ CallID, Label, Status string }

// AgentEvent.Kind: answer.delta, tool.started, tool.finished, draft.proposed.
// draft.proposed is internal: HTTP emits action.proposed only AFTER SaveAndLink.
type AgentEvent struct {
	Kind  string
	Text  string
	Tool  *ToolEvent
	Draft *DraftInput
}
type Emit func(context.Context, AgentEvent) error
type Agent interface {
	Run(context.Context, AgentInput, Emit) (AgentResult, error)
}
type Knowledge interface {
	Search(context.Context, string) ([]Source, error)
	Read(context.Context, string) (Document, error)
}
type TaskProvider interface {
	FirstTask(context.Context) (Task, error)
}
type AccessCheck struct{ ResourceID, Permission string }
type AccessSubmission struct {
	RequestID string // stable idempotency key, never changed on timeout/retry
	Resource  Resource
	Reason    string
}
type AccessResult struct{ ExternalID, Status, FailureReason string }
type AccessProvider interface {
	Check(context.Context, AccessCheck) (bool, error)
	// Submit is called only by the worker for a durably confirmed operation.
	Submit(context.Context, AccessSubmission) (AccessResult, error)
	Lookup(context.Context, string) (AccessResult, error) // request_id, not external_id
}
type DraftInput struct {
	RequestID, ConversationID, AssistantMessageID string
	StepID                                        *string
	Resource                                      Resource
	Reason                                        string
}
type Draft struct {
	DraftInput
	Version, Revision int64
}
type DraftService interface {
	// SaveAndLink atomically persists draft and assistant.request_ids before emit.
	SaveAndLink(context.Context, DraftInput) (Draft, error)
}
