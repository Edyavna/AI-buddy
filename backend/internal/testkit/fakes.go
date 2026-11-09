// Package testkit provides deterministic, injectable doubles, not production adapters.
package testkit

import (
	"context"
	"sync"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

const UserID = "intern_demo_1"
const TeamID = "team_demo_backend"
const ResourceID = "repo_demo_backend"
const DocumentID = "doc_local_setup"

func Context(ctx context.Context) context.Context {
	return identity.WithPrincipal(ctx, identity.Principal{UserID: UserID, TeamID: TeamID, Role: identity.Intern})
}

func authorize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := identity.Require(ctx)
	if err != nil {
		return err
	}
	if p.UserID != UserID || p.TeamID != TeamID {
		return ports.ErrNotFound
	}
	if p.Role != identity.Intern {
		return ports.ErrForbidden
	}
	return nil
}

type Agent struct {
	Events []ports.AgentEvent
	Result ports.AgentResult
	Err    error
}

// Drafts adapts a test callback; the real coordinator is drafts.Service.
// A nil callback fails explicitly instead of pretending persistence succeeded.
type Drafts func(context.Context, ports.DraftInput) (ports.Draft, error)

func (d Drafts) SaveAndLink(ctx context.Context, in ports.DraftInput) (ports.Draft, error) {
	if err := authorize(ctx); err != nil {
		return ports.Draft{}, err
	}
	if d == nil {
		return ports.Draft{}, ports.ErrUnavailable
	}
	return d(ctx, in)
}

func (a Agent) Run(ctx context.Context, in ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
	if err := authorize(ctx); err != nil {
		return ports.AgentResult{}, err
	}
	for _, event := range a.Events {
		if err := ctx.Err(); err != nil {
			return ports.AgentResult{}, err
		}
		if err := emit(ctx, event); err != nil {
			return ports.AgentResult{}, err
		}
	}
	return a.Result, a.Err
}

type Knowledge struct{}

func (Knowledge) Search(ctx context.Context, query string) ([]ports.Source, error) {
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	if query == "" {
		return []ports.Source{}, nil
	}
	return []ports.Source{{ID: "source_1", DocumentID: DocumentID, Title: "Тестовая инструкция запуска", Excerpt: "Для запуска нужен доступ к репозиторию."}}, nil
}
func (Knowledge) Read(ctx context.Context, id string) (ports.Document, error) {
	if err := authorize(ctx); err != nil {
		return ports.Document{}, err
	}
	if id != DocumentID {
		return ports.Document{}, ports.ErrNotFound
	}
	return ports.Document{ID: DocumentID, Title: "Тестовая инструкция запуска", Content: "Синтетические данные. Для запуска нужен доступ к репозиторию."}, nil
}

type Tasks struct{}

func (Tasks) FirstTask(ctx context.Context) (ports.Task, error) {
	if err := authorize(ctx); err != nil {
		return ports.Task{}, err
	}
	return ports.Task{ID: "task_demo_1", Title: "Подготовить окружение", Resource: ports.Resource{ID: ResourceID, Name: "Тестовый репозиторий", Permission: "read"}}, nil
}

// Access simulates idempotent creation. No scheduler or approval is hidden here.
// Its zero value is ready to use; one instance should be injected per test.
type Access struct {
	mu    sync.Mutex
	items map[string]accessItem
}
type accessItem struct {
	input  ports.AccessSubmission
	result ports.AccessResult
}

func (a *Access) Check(ctx context.Context, check ports.AccessCheck) (bool, error) {
	if err := authorize(ctx); err != nil {
		return false, err
	}
	if check.ResourceID != ResourceID || (check.Permission != "read" && check.Permission != "write") {
		return false, ports.ErrNotFound
	}
	return false, nil
}
func (a *Access) Submit(ctx context.Context, in ports.AccessSubmission) (ports.AccessResult, error) {
	if err := authorize(ctx); err != nil {
		return ports.AccessResult{}, err
	}
	if in.RequestID == "" || in.Reason == "" {
		return ports.AccessResult{}, ports.ErrInvalid
	}
	if _, err := a.Check(ctx, ports.AccessCheck{ResourceID: in.Resource.ID, Permission: in.Resource.Permission}); err != nil {
		return ports.AccessResult{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.items == nil {
		a.items = make(map[string]accessItem)
	}
	if item, ok := a.items[in.RequestID]; ok {
		if item.input != in {
			return ports.AccessResult{}, ports.ErrConflict
		}
		return item.result, nil
	}
	result := ports.AccessResult{ExternalID: "FAKE-" + in.RequestID, Status: "pending"}
	a.items[in.RequestID] = accessItem{input: in, result: result}
	return result, nil
}
func (a *Access) Lookup(ctx context.Context, requestID string) (ports.AccessResult, error) {
	if err := authorize(ctx); err != nil {
		return ports.AccessResult{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	item, ok := a.items[requestID]
	if !ok {
		return ports.AccessResult{}, ports.ErrNotFound
	}
	return item.result, nil
}

var (
	_ ports.Agent          = Agent{}
	_ ports.Knowledge      = Knowledge{}
	_ ports.TaskProvider   = Tasks{}
	_ ports.AccessProvider = (*Access)(nil)
	_ ports.DraftService   = Drafts(nil)
)
