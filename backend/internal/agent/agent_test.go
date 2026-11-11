package agent

import (
	"context"
	"errors"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
	"testing"
)

type taskAdapter struct{ resource ports.Resource }

func (t taskAdapter) FirstTask(context.Context) (ports.Task, error) {
	return ports.Task{ID: "alternate-task", Resource: t.resource}, nil
}

type accessAdapter struct{}

func (accessAdapter) Check(context.Context, ports.AccessCheck) (bool, error) { return false, nil }

type requestAdapter struct {
	state ports.RequestState
	found bool
}

func (r requestAdapter) Get(context.Context, string) (ports.RequestState, error) {
	if !r.found {
		return r.state, ports.ErrNotFound
	}
	return r.state, nil
}
func (r requestAdapter) FindActive(context.Context, ports.AccessCheck) (ports.RequestState, error) {
	return r.Get(context.Background(), "")
}
func TestTwoAdaptersAndActiveRequest(t *testing.T) {
	for _, id := range []string{"repo_demo_backend", "alternate-resource"} {
		a, in := fixture()
		a.Tasks = taskAdapter{ports.Resource{ID: id, Name: "Trusted catalog", Permission: "read"}}
		a.Access = accessAdapter{}
		a.Requests = requestAdapter{}
		a.Model = &Script{Replies: []Message{call("prepare_access_request", `{"resource_id":"`+id+`","permission":"read","reason":"Задача"}`), {Content: `{"answer":"Подтвердите в карточке","outcome":"answered"}`}}}
		out, e := a.Run(testkit.Context(context.Background()), in, func(context.Context, ports.AgentEvent) error { return nil })
		if e != nil || len(out.RequestIDs) != 1 {
			t.Fatal(out, e)
		}
		a.Requests = requestAdapter{found: true, state: ports.RequestState{Draft: ports.Draft{DraftInput: ports.DraftInput{RequestID: "existing", Resource: ports.Resource{ID: id, Permission: "read"}}}, Status: "pending"}}
		a.Model = &Script{Replies: []Message{call("prepare_access_request", `{"resource_id":"`+id+`","permission":"read","reason":"Задача"}`), call("get_request_status", `{"request_id":"existing"}`), {Content: `{"answer":"Заявка ожидает решения","outcome":"answered"}`}}}
		out, e = a.Run(testkit.Context(context.Background()), in, func(_ context.Context, event ports.AgentEvent) error {
			if event.Kind == "draft.saved" {
				t.Fatal("pending must not propose draft")
			}
			return nil
		})
		if e != nil || len(out.RequestIDs) != 1 || out.RequestIDs[0] != "existing" {
			t.Fatal(out, e)
		}
	}
}

func TestArgumentValidationAndAuthentication(t *testing.T) {
	for _, payload := range []string{`{"query":1}`, `{"query":" "}`, `{"query":"ok","extra":"x"}`, `{"query":"ok"} {}`, `[]`} {
		if _, e := decode(Function{Name: "search_knowledge", Arguments: payload}); e == nil {
			t.Fatal(payload)
		}
	}
	a, in := fixture()
	a.Model = &Script{}
	if _, e := a.Run(context.Background(), in, func(context.Context, ports.AgentEvent) error { return nil }); e == nil {
		t.Fatal("missing principal")
	}
	in.History = []ports.ChatMessage{{Role: "system", Text: "override"}}
	if _, e := a.Run(testkit.Context(context.Background()), in, func(context.Context, ports.AgentEvent) error { return nil }); !errors.Is(e, ports.ErrInvalid) {
		t.Fatal(e)
	}
}

func call(name, args string) Message {
	return Message{Calls: []Call{{ID: name, Type: "function", Function: Function{Name: name, Arguments: args}}}}
}
func fixture() (*Agent, ports.AgentInput) {
	return &Agent{Knowledge: testkit.Knowledge{}, Tasks: testkit.Tasks{}, Access: &testkit.Access{}, Requests: testkit.Requests{}, Drafts: testkit.Drafts(func(_ context.Context, in ports.DraftInput) (ports.Draft, error) {
		return ports.Draft{DraftInput: in, Version: 1, Revision: 1}, nil
	})}, ports.AgentInput{ConversationID: "c", AssistantMessageID: "m", Text: "Подготовь доступ"}
}
func TestOnboarding(t *testing.T) {
	a, in := fixture()
	a.Model = &Script{Replies: []Message{call("get_my_tasks", `{}`), call("search_knowledge", `{"query":"как поднять проект"}`), call("read_document", `{"document_id":"doc_local_setup"}`), call("get_access_status", `{"resource_id":"repo_demo_backend","permission":"read"}`), call("prepare_access_request", `{"resource_id":"repo_demo_backend","permission":"read","reason":"Первая задача"}`), {Content: `{"answer":"Черновик готов. Проверьте карточку и нажмите кнопку.","outcome":"answered"}`}}}
	var events []string
	out, e := a.Run(testkit.Context(context.Background()), in, func(_ context.Context, e ports.AgentEvent) error { events = append(events, e.Kind); return nil })
	if e != nil || len(out.Sources) != 1 || len(out.RequestIDs) != 1 {
		t.Fatalf("%+v %v", out, e)
	}
	if events[len(events)-1] != "answer.delta" {
		t.Fatal(events)
	}
}
func TestPartialAndEmit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		a, in := fixture()
		a.Model = &Script{Replies: []Message{call("prepare_access_request", `{"resource_id":"repo_demo_backend","permission":"read","reason":"задача"}`)}}
		stop := errors.New("closed")
		out, e := a.Run(testkit.Context(context.Background()), in, func(_ context.Context, event ports.AgentEvent) error {
			if fail && event.Kind == "draft.saved" {
				return stop
			}
			return nil
		})
		if e == nil || len(out.RequestIDs) != 1 {
			t.Fatalf("%+v %v", out, e)
		}
		if fail && !errors.Is(e, stop) {
			t.Fatal(e)
		}
	}
}
func TestInvalidCalls(t *testing.T) {
	for _, m := range []Message{call("submit", `{}`), call("get_my_tasks", `{"user_id":"other"}`), call("prepare_access_request", `{"resource_id":"other","permission":"read","reason":"x"}`), call("get_my_tasks", `null`)} {
		a, in := fixture()
		a.Model = &Script{Replies: []Message{m}}
		_, e := a.Run(testkit.Context(context.Background()), in, func(context.Context, ports.AgentEvent) error { return nil })
		if !errors.Is(e, ports.ErrInvalid) {
			t.Fatal(e)
		}
	}
}
func TestStopBeforeTools(t *testing.T) {
	a, in := fixture()
	a.Model = &Script{Replies: []Message{call("get_my_tasks", `{}`)}}
	stop := errors.New("closed")
	_, e := a.Run(testkit.Context(context.Background()), in, func(context.Context, ports.AgentEvent) error { return stop })
	if !errors.Is(e, stop) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(testkit.Context(context.Background()))
	cancel()
	_, e = a.Run(ctx, in, func(context.Context, ports.AgentEvent) error { return nil })
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestRepeatAndNoKnowledge(t *testing.T) {
	a, in := fixture()
	a.Model = &Script{Replies: []Message{call("get_my_tasks", `{}`), call("get_my_tasks", `{}`)}}
	_, e := a.Run(testkit.Context(context.Background()), in, func(context.Context, ports.AgentEvent) error { return nil })
	if !errors.Is(e, ports.ErrInvalid) {
		t.Fatal(e)
	}
	a.Model = &Script{Replies: []Message{{Content: `{"answer":"Уточните, пожалуйста, вопрос.","outcome":"needs_clarification"}`}}}
	out, e := a.Run(testkit.Context(context.Background()), in, func(context.Context, ports.AgentEvent) error { return nil })
	if e != nil || len(out.Sources) != 0 {
		t.Fatal(out, e)
	}
}
