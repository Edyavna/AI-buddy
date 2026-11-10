package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type recordingSink struct {
	mu       sync.Mutex
	kinds    []string
	data     []any
	seen     chan string
	terminal func(string, any) error
}

func newSink() *recordingSink { return &recordingSink{seen: make(chan string, 64)} }
func (s *recordingSink) Event(kind string, data any) error {
	if s.terminal != nil && strings.HasPrefix(kind, "message.") && kind != "message.started" {
		if err := s.terminal(kind, data); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.kinds = append(s.kinds, kind)
	s.data = append(s.data, data)
	s.mu.Unlock()
	s.seen <- kind
	return nil
}
func (s *recordingSink) Heartbeat() error { s.seen <- "heartbeat"; return nil }
func (s *recordingSink) contains(kind string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.kinds {
		if k == kind {
			return true
		}
	}
	return false
}
func waitKind(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case kind := <-ch:
			if kind == want {
				return
			}
		case <-timer.C:
			t.Fatal("did not receive", want)
			return
		}
	}
}
func waitDone(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop")
	}
}

type observedStore struct {
	Store
	saved     chan struct{}
	failFinal bool
}

func (s observedStore) SavePartial(ctx context.Context, p identity.Principal, c, id, text string) error {
	err := s.Store.SavePartial(ctx, p, c, id, text)
	if err == nil {
		select {
		case s.saved <- struct{}{}:
		default:
		}
	}
	return err
}
func (s observedStore) Finish(ctx context.Context, p identity.Principal, c string, m Message) (Message, error) {
	if s.failFinal {
		return Message{}, ports.ErrUnavailable
	}
	return s.Store.Finish(ctx, p, c, m)
}

type forcedDeadline struct{ context.Context }

func (c forcedDeadline) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}
func TestPostgresStreamingTimersAndCancellation(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint("timeout=", timeout), func(t *testing.T) {
			heartbeat, persist := make(chan time.Time, 1), make(chan time.Time, 1)
			trigger := make(chan context.CancelFunc, 1)
			options := StreamOptions{Tick: func(d time.Duration) (<-chan time.Time, func()) {
				if d == 15*time.Second {
					return heartbeat, func() {}
				}
				return persist, func() {}
			}}
			if timeout {
				options.Deadline = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
					c, cancel := context.WithCancel(ctx)
					trigger <- cancel
					return forcedDeadline{c}, cancel
				}
			}
			agent := agentFunc(func(ctx context.Context, _ ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
				if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: "Часть 🚀"}); err != nil {
					return ports.AgentResult{}, err
				}
				<-ctx.Done()
				return ports.AgentResult{Sources: []ports.Source{{ID: "s", DocumentID: "doc", Title: "Источник", Excerpt: "Часть"}}}, ctx.Err()
			})
			f := newFixture(t, agent, nil, options)
			c := f.create(t, 1)
			ctx, cancel := context.WithCancel(testContext())
			defer cancel()
			p, err := f.service.Prepare(ctx, c.ID, uid(2), "Вопрос")
			if err != nil {
				t.Fatal(err)
			}
			saved := make(chan struct{}, 1)
			svc := f.service
			svc.Store = observedStore{Store: f.store, saved: saved}
			sink := newSink()
			done := make(chan error, 1)
			go func() { done <- svc.Stream(ctx, p, sink, options) }()
			waitKind(t, sink.seen, "answer.delta")
			heartbeat <- time.Now()
			waitKind(t, sink.seen, "heartbeat")
			persist <- time.Now()
			select {
			case <-saved:
			case <-time.After(5 * time.Second):
				t.Fatal("partial not saved")
			}
			messages, err := f.store.Messages(ctx, testOwner(), c.ID)
			if err != nil || messages[1].Text != "Часть 🚀" || messages[1].Status != "generating" {
				t.Fatal(messages, err)
			}
			if timeout {
				(<-trigger)()
			} else {
				cancel()
			}
			waitDone(t, done)
			messages, err = f.store.Messages(testContext(), testOwner(), c.ID)
			if err != nil {
				t.Fatal(err)
			}
			m := messages[1]
			wantStatus, wantCode := "interrupted", "GENERATION_INTERRUPTED"
			if timeout {
				wantStatus, wantCode = "failed", "GENERATION_TIMEOUT"
			}
			if m.Status != wantStatus || m.Error == nil || m.Error.Error.Code != wantCode || m.Text != "Часть 🚀" || len(m.Sources) != 1 {
				t.Fatal(m)
			}
			if timeout && !sink.contains("message.failed") {
				t.Fatal("missing timeout terminal event")
			}
			if !timeout && (sink.contains("message.failed") || sink.contains("message.completed")) {
				t.Fatal("sent event after disconnect")
			}
		})
	}
}
func TestPostgresTerminalCommitAndFailure(t *testing.T) {
	f := newFixture(t, fastAgent(), nil, StreamOptions{})
	c := f.create(t, 1)
	ctx, cancel := context.WithCancel(testContext())
	defer cancel()
	p, err := f.service.Prepare(ctx, c.ID, uid(2), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	sink := newSink()
	sink.terminal = func(kind string, data any) error {
		messages, err := f.store.Messages(testContext(), testOwner(), c.ID)
		if err != nil {
			return err
		}
		if messages[1].Status != "completed" {
			return errors.New("terminal before commit")
		}
		cancel()
		return nil
	}
	if err := f.service.Stream(ctx, p, sink, StreamOptions{}); err != nil {
		t.Fatal(err)
	}
	late := p.Assistant
	late.Status = "interrupted"
	late.Error = failure("GENERATION_INTERRUPTED", "trace")
	kept, err := f.store.Finish(testContext(), testOwner(), c.ID, late)
	if err != nil || kept.Status != "completed" {
		t.Fatal("late disconnect overwrote terminal", kept, err)
	}
	c = f.create(t, 3)
	p, err = f.service.Prepare(testContext(), c.ID, uid(4), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	svc := f.service
	svc.Store = observedStore{Store: f.store, failFinal: true}
	sink = newSink()
	if err := svc.Stream(testContext(), p, sink, StreamOptions{}); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	if sink.contains("message.completed") || sink.contains("message.failed") {
		t.Fatal("unsaved terminal emitted")
	}
}
func TestPostgresInvalidAgentEventsAndPanic(t *testing.T) {
	for _, name := range []string{"unknown", "tool", "panic", "error"} {
		t.Run(name, func(t *testing.T) {
			a := agentFunc(func(ctx context.Context, _ ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
				switch name {
				case "unknown":
					return ports.AgentResult{}, emit(ctx, ports.AgentEvent{Kind: "hidden.reasoning", Text: "secret"})
				case "tool":
					return ports.AgentResult{}, emit(ctx, ports.AgentEvent{Kind: "tool.finished", Tool: &ports.ToolEvent{CallID: "missing", Status: "succeeded"}})
				case "panic":
					panic("sk-secret")
				default:
					return ports.AgentResult{}, errors.New("sk-secret")
				}
			})
			f := newFixture(t, a, nil, StreamOptions{})
			c := f.create(t, 1)
			w := f.call(t, "POST", "/conversations/"+c.ID+"/messages", `{"client_message_id":"`+uid(2)+`","text":"Hi"}`, f.cookie, 200)
			if !strings.Contains(w.Body.String(), "message.failed") || strings.Contains(w.Body.String(), "sk-secret") || strings.Contains(w.Body.String(), "hidden.reasoning") {
				t.Fatal(w.Body.String())
			}
		})
	}
}

func TestPostgresEmitterSnapshotsReusedEvent(t *testing.T) {
	a := agentFunc(func(ctx context.Context, _ ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
		tool := &ports.ToolEvent{CallID: "call", Label: "Ищу инструкции"}
		if err := emit(ctx, ports.AgentEvent{Kind: "tool.started", Tool: tool}); err != nil {
			return ports.AgentResult{}, err
		}
		tool.Label = ""
		tool.Status = "succeeded"
		return ports.AgentResult{}, emit(ctx, ports.AgentEvent{Kind: "tool.finished", Tool: tool})
	})
	f := newFixture(t, a, nil, StreamOptions{})
	c := f.create(t, 1)
	w := f.call(t, "POST", "/conversations/"+c.ID+"/messages", `{"client_message_id":"`+uid(2)+`","text":"Hi"}`, f.cookie, 200)
	if !strings.Contains(w.Body.String(), "message.completed") || !strings.Contains(w.Body.String(), "Ищу инструкции") {
		t.Fatal(w.Body.String())
	}
}
func readFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(line)
		if line == "\n" {
			return b.String()
		}
	}
}
func TestPostgresRealHTTPFlushAndDisconnect(t *testing.T) {
	heartbeat := make(chan time.Time, 1)
	options := StreamOptions{Tick: func(d time.Duration) (<-chan time.Time, func()) {
		if d == 15*time.Second {
			return heartbeat, func() {}
		}
		return make(chan time.Time), func() {}
	}}
	a := agentFunc(func(ctx context.Context, _ ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
		if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: "Живой поток"}); err != nil {
			return ports.AgentResult{}, err
		}
		<-ctx.Done()
		return ports.AgentResult{}, ctx.Err()
	})
	f := newFixture(t, a, nil, options)
	c := f.create(t, 1)
	returned := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.handler.ServeHTTP(w, r)
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/messages") {
			close(returned)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/conversations/"+c.ID+"/messages", strings.NewReader(`{"client_message_id":"`+uid(2)+`","text":"Hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")
	req.AddCookie(f.cookie)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if frame := readFrame(t, reader); !strings.Contains(frame, "message.started") {
		t.Fatal(frame)
	}
	if frame := readFrame(t, reader); !strings.Contains(frame, "answer.delta") {
		t.Fatal(frame)
	}
	heartbeat <- time.Now()
	if frame := readFrame(t, reader); frame != ": heartbeat\n\n" {
		t.Fatal(frame)
	}
	cancel()
	response.Body.Close()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish on disconnect")
	}
	messages, err := f.store.Messages(testContext(), testOwner(), c.ID)
	if err != nil || messages[1].Status != "interrupted" || messages[1].Text != "Живой поток" {
		t.Fatal(messages, err)
	}
}
