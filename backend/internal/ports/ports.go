// Package ports defines internal Go seams. These are not HTTP/SSE DTOs.
package ports

import (
	"context"
	"errors"
	"time"
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
type Document struct {
	ID, Title, Content string
	UpdatedAt          time.Time
}
type Task struct {
	ID, Title, Description, URL string
	Resource                    Resource
}
type ChatMessage struct{ Role, Text string }
type AgentInput struct {
	// Requests are server-loaded snapshots, not model-supplied state.
	Requests                                 []RequestState
	ConversationID, AssistantMessageID, Text string
	StepID                                   *string
	History                                  []ChatMessage
}

// Return collected Sources/RequestIDs even with an error after partial work.
type AgentResult struct {
	Sources    []Source
	RequestIDs []string
}
type ToolEvent struct{ CallID, Label, Status string }

// AgentEvent.Kind: answer.delta, tool.started, tool.finished, draft.saved.
// draft.saved carries the committed result of DraftService.SaveAndLink.
// HTTP maps it to action.proposed; the agent never writes HTTP or SQL directly.
type AgentEvent struct {
	Kind  string
	Text  string
	Tool  *ToolEvent
	Draft *Draft
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

// Only this read-only interface is injected into the agent.
type AccessChecker interface {
	Check(context.Context, AccessCheck) (bool, error)
}
type AccessProvisioner interface {
	// Submit is called only by the worker for a durably confirmed operation.
	Submit(context.Context, AccessSubmission) (AccessResult, error)
	Lookup(context.Context, string) (AccessResult, error) // request_id, not external_id
}
type AccessProvider interface {
	AccessChecker
	AccessProvisioner
}
type DraftInput struct {
	RequestID, ConversationID, AssistantMessageID string
	StepID                                        *string
	Resource                                      Resource
	Reason                                        string
}
type Draft struct {
	DraftInput
	Version, Revision    int64
	CreatedAt, UpdatedAt time.Time
}

// RequestState is a saved snapshot, mapped to AccessRequest HTTP DTOs.
// Draft fields retain parameters/links when the status is no longer draft.
type RequestState struct {
	Draft
	Status          string // draft/submitting/pending/granted/rejected/failed
	ExternalID      *string
	StatusCheckedAt *time.Time
	FailureReason   string
}
type RequestReader interface {
	Get(context.Context, string) (RequestState, error)
	// FindActive returns draft/submitting/pending for this user's resource/permission.
	FindActive(context.Context, AccessCheck) (RequestState, error)
}
type DraftService interface {
	// SaveAndLink atomically persists draft and assistant.request_ids before emit.
	SaveAndLink(context.Context, DraftInput) (Draft, error)
}
