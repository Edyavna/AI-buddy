package chat

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Prepared struct {
	Reservation
	Input     ports.AgentInput
	StartedAt time.Time
}

func (s Service) List(ctx context.Context) ([]Conversation, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	return s.Store.List(ctx, p)
}
func (s Service) Get(ctx context.Context, id string) (Conversation, error) {
	p, err := principal(ctx)
	if err != nil {
		return Conversation{}, err
	}
	return s.Store.Get(ctx, p, id)
}
func (s Service) Messages(ctx context.Context, id string) ([]Message, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.Store.Messages(ctx, p, id)
	if err == nil {
		s.checkSources(ctx, items)
	}
	return items, err
}
func (s Service) Create(ctx context.Context, id string, step *string) (Conversation, bool, error) {
	p, err := principal(ctx)
	if err != nil {
		return Conversation{}, false, err
	}
	if !validUUID(id) || (step != nil && *step == "") {
		return Conversation{}, false, ports.ErrInvalid
	}
	return s.Store.Create(ctx, p, strings.ToLower(id), step)
}
func failure(code, trace string) *APIError {
	messages := map[string]string{
		"GENERATION_INTERRUPTED": "Генерация прервана; результат доступен в истории",
		"GENERATION_TIMEOUT":     "Превышено время ожидания ответа",
		"LLM_UNAVAILABLE":        "Модель временно недоступна",
		"TOOL_FAILED":            "Не удалось завершить обработку запроса",
		"SERVICE_UNAVAILABLE":    "Не удалось сохранить ответ",
	}
	return &APIError{Error: httpapi.Error{Code: code, Message: messages[code], TraceID: trace}}
}
func (s Service) request(ctx context.Context, id string) (ports.RequestState, error) {
	if s.Requests == nil {
		return ports.RequestState{}, ports.ErrUnavailable
	}
	r, err := s.Requests.Get(ctx, id)
	if err != nil {
		return r, err
	}
	if r.RequestID != id {
		return r, ports.ErrInvalid
	}
	return r, nil
}
func (s Service) Prepare(ctx context.Context, id, clientID, text string) (Prepared, error) {
	p, err := principal(ctx)
	if err != nil {
		return Prepared{}, err
	}
	if _, err := s.Store.Get(ctx, p, id); err != nil {
		return Prepared{}, err
	}
	if !validUUID(clientID) || !validText(text) {
		return Prepared{}, ports.ErrInvalid
	}
	if s.Agent == nil {
		return Prepared{}, ports.ErrUnavailable
	}
	started := time.Now()
	work, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	r, err := s.Store.Begin(work, p, id, strings.ToLower(clientID), text, httpapi.TraceID(ctx))
	if err != nil {
		return Prepared{}, err
	}
	s.checkSources(work, r.Previous)
	in := ports.AgentInput{ConversationID: id, AssistantMessageID: r.Assistant.ID, StepID: r.Conversation.StepID, Text: text, History: history(r.Previous, text), Requests: []ports.RequestState{}}
	ids := map[string]bool{}
	for _, m := range r.Previous {
		for _, id := range m.RequestIDs {
			if ids[id] {
				continue
			}
			ids[id] = true
			request, err := s.request(work, id)
			if err != nil {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				m := r.Assistant
				m.Status = "failed"
				m.Error = failure("TOOL_FAILED", httpapi.TraceID(ctx))
				if _, saveErr := s.Store.Finish(cleanup, p, r.Conversation.ID, m); saveErr != nil {
					return Prepared{}, saveErr
				}
				return Prepared{}, ports.ErrUnavailable
			}
			in.Requests = append(in.Requests, request)
		}
	}
	return Prepared{Reservation: r, Input: in, StartedAt: started}, nil
}

func (s Service) proposed(ctx context.Context, p Prepared, event ports.AgentEvent) (*httpapi.AccessRequest, error) {
	if event.Draft == nil || event.Draft.RequestID == "" {
		return nil, ports.ErrInvalid
	}
	request, err := s.request(ctx, event.Draft.RequestID)
	if err != nil {
		return nil, err
	}
	if request.ConversationID != p.Conversation.ID || request.AssistantMessageID != p.Assistant.ID || !sameStep(request.StepID, p.Conversation.StepID) {
		return nil, ports.ErrForbidden
	}
	owner, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	saved, err := s.Store.Messages(ctx, owner, p.Conversation.ID)
	if err != nil {
		return nil, err
	}
	linked := false
	for _, m := range saved {
		if m.ID == p.Assistant.ID {
			linked = slices.Contains(m.RequestIDs, request.RequestID)
		}
	}
	if !linked {
		return nil, ports.ErrConflict
	}
	if request.Status != "draft" {
		return nil, nil
	}
	if request.Version < 1 || request.Revision < 1 || request.CreatedAt.IsZero() || request.UpdatedAt.IsZero() || request.ExternalID != nil || request.Resource.ID == "" || request.Resource.Name == "" || request.Reason == "" || (request.Resource.Permission != "read" && request.Resource.Permission != "write") {
		return nil, ports.ErrInvalid
	}
	dto := httpapi.RequestDTO(request)
	return &dto, nil
}

const (
	maxGenerationTimeout = 120 * time.Second
	maxCleanupTimeout    = 5 * time.Second
	maxWriteTimeout      = 5 * time.Second
	// A pending write may finish after the generation deadline, followed by cleanup.
	generationLease = maxGenerationTimeout + maxWriteTimeout + maxCleanupTimeout
)

// StreamOptions are server/test settings, not client-controlled timeouts.
// Durations may shorten but never exceed the server budgets used by the lease.
// Tick and Deadline permit deterministic tests without waiting 15/120 seconds.
type StreamOptions struct {
	GenerationTimeout, Heartbeat, SaveInterval, WriteTimeout, CleanupTimeout time.Duration
	Tick                                                                     func(time.Duration) (<-chan time.Time, func())
	Deadline                                                                 func(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

func (o StreamOptions) defaults() StreamOptions {
	if o.GenerationTimeout <= 0 || o.GenerationTimeout > maxGenerationTimeout {
		o.GenerationTimeout = maxGenerationTimeout
	}
	if o.Heartbeat <= 0 {
		o.Heartbeat = 15 * time.Second
	}
	if o.SaveInterval <= 0 {
		o.SaveInterval = time.Second
	}
	if o.WriteTimeout <= 0 || o.WriteTimeout > maxWriteTimeout {
		o.WriteTimeout = maxWriteTimeout
	}
	if o.CleanupTimeout <= 0 || o.CleanupTimeout > maxCleanupTimeout {
		o.CleanupTimeout = maxCleanupTimeout
	}
	if o.Tick == nil {
		o.Tick = func(d time.Duration) (<-chan time.Time, func()) { t := time.NewTicker(d); return t.C, t.Stop }
	}
	if o.Deadline == nil {
		o.Deadline = context.WithTimeout
	}
	return o
}

type Sink interface {
	Event(string, any) error
	Heartbeat() error
}
type runResult struct {
	result ports.AgentResult
	err    error
}

// Stream is the sole writer/persistence coordinator. Agent callbacks never touch
// ResponseWriter, and bounded sends unblock when the generation is cancelled.
func (s Service) Stream(ctx context.Context, p Prepared, sink Sink, options StreamOptions) error {
	owner, err := principal(ctx)
	if err != nil {
		return err
	}
	o := options.defaults()
	timeout := o.GenerationTimeout
	if !p.StartedAt.IsZero() {
		timeout -= time.Since(p.StartedAt)
	}
	runCtx, cancel := o.Deadline(ctx, timeout)
	defer cancel()
	events := make(chan ports.AgentEvent, 16)
	done := make(chan runResult, 1)
	go func() {
		defer close(events)
		defer func() {
			if recover() != nil {
				done <- runResult{err: errors.New("agent panic")}
			}
		}()
		result, err := s.Agent.Run(runCtx, p.Input, func(eventCtx context.Context, event ports.AgentEvent) error {
			// A sequential caller may reuse its pointed-to event after emit returns.
			// The queue must own a snapshot, not references the agent can mutate next.
			if event.Tool != nil {
				tool := *event.Tool
				event.Tool = &tool
			}
			if event.Draft != nil {
				draft := *event.Draft
				if draft.StepID != nil {
					step := *draft.StepID
					draft.StepID = &step
				}
				event.Draft = &draft
			}
			select {
			case <-runCtx.Done():
				return runCtx.Err()
			case <-eventCtx.Done():
				return eventCtx.Err()
			case events <- event:
				return nil
			}
		})
		done <- runResult{result, err}
	}()
	heartbeat, stopHeartbeat := o.Tick(o.Heartbeat)
	defer stopHeartbeat()
	save, stopSave := o.Tick(o.SaveInterval)
	defer stopSave()
	message := p.Assistant
	var text strings.Builder
	tools := map[string]bool{}
	var result ports.AgentResult
	var cause error
	disconnected, finished := false, false
	storageFailure := false
	if err := sink.Event("message.started", map[string]string{"user_message_id": p.User.ID, "message_id": p.Assistant.ID}); err != nil {
		cause = err
		disconnected = true
	}
	for cause == nil && !finished {
		select {
		case <-runCtx.Done():
			cause = runCtx.Err()
			disconnected = ctx.Err() != nil
		case <-heartbeat:
			if err := sink.Heartbeat(); err != nil {
				cause = err
				disconnected = true
			}
		case <-save:
			opCtx, stop := context.WithTimeout(runCtx, o.WriteTimeout)
			err := s.Store.SavePartial(opCtx, owner, p.Conversation.ID, message.ID, text.String())
			stop()
			if err != nil {
				cause = err
				storageFailure = true
			}
		case event, ok := <-events:
			if !ok {
				r := <-done
				result, cause, finished = r.result, r.err, true
				continue
			}
			var data any
			switch event.Kind {
			case "answer.delta":
				text.WriteString(event.Text)
				data = map[string]string{"message_id": message.ID, "text": event.Text}
			case "tool.started":
				if event.Tool == nil || event.Tool.CallID == "" || event.Tool.Label == "" || tools[event.Tool.CallID] {
					cause = ports.ErrInvalid
					break
				}
				tools[event.Tool.CallID] = true
				data = map[string]string{"message_id": message.ID, "tool_call_id": event.Tool.CallID, "label": event.Tool.Label}
			case "tool.finished":
				if event.Tool == nil || !tools[event.Tool.CallID] || (event.Tool.Status != "succeeded" && event.Tool.Status != "failed") {
					cause = ports.ErrInvalid
					break
				}
				delete(tools, event.Tool.CallID)
				data = map[string]string{"message_id": message.ID, "tool_call_id": event.Tool.CallID, "status": event.Tool.Status}
			case "draft.saved":
				checkCtx, stop := context.WithTimeout(runCtx, o.WriteTimeout)
				request, err := s.proposed(checkCtx, p, event)
				stop()
				if err != nil {
					cause = err
					break
				}
				if request != nil {
					if err := sink.Event("action.proposed", map[string]any{"message_id": message.ID, "request": request}); err != nil {
						cause = err
						disconnected = true
					}
				}
			default:
				cause = ports.ErrInvalid
			}
			if cause == nil && data != nil {
				if err := sink.Event(event.Kind, data); err != nil {
					cause = err
					disconnected = true
				}
			}
		}
	}
	if cause == nil && runCtx.Err() != nil {
		cause = runCtx.Err()
		disconnected = ctx.Err() != nil
	}
	timedOut := errors.Is(cause, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded)
	cancel()
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), o.CleanupTimeout)
	defer stop()
	if !finished {
		// Allow a cooperative agent to return collected metadata. Reserve most of the
		// cleanup budget for the durable terminal write even if a provider misbehaves.
		wait := time.NewTimer(o.CleanupTimeout / 5)
		defer wait.Stop()
	collect:
		for {
			select {
			case event, ok := <-events:
				if !ok {
					r := <-done
					result = r.result
					break collect
				}
				if event.Kind == "answer.delta" {
					text.WriteString(event.Text)
				}
			case <-wait.C:
				break collect
			case <-cleanup.Done():
				break collect
			}
		}
	}
	message.Text = text.String()
	if finished && result.PrivateAnswer != "" {
		message.Text = result.PrivateAnswer
	}
	message.Routing = result.Routing
	message.Sources = []Source{}
	message.RequestIDs = []string{}
	for _, source := range result.Sources {
		if source.ID == "" || source.DocumentID == "" {
			cause = ports.ErrInvalid
			continue
		}
		item := Source{ID: source.ID, DocumentID: source.DocumentID, Title: source.Title, Excerpt: source.Excerpt, Direction: source.Direction, Version: source.Version}
		if !source.UpdatedAt.IsZero() {
			item.UpdatedAt = &source.UpdatedAt
		}
		message.Sources = append(message.Sources, item)
	}
	for _, id := range result.RequestIDs {
		if slices.Contains(message.RequestIDs, id) {
			continue
		}
		if _, err := s.request(cleanup, id); err != nil {
			cause = err
			continue
		}
		message.RequestIDs = append(message.RequestIDs, id)
	}
	message.Status = "completed"
	if disconnected || ctx.Err() != nil && !finished {
		message.Status = "interrupted"
		message.Error = failure("GENERATION_INTERRUPTED", httpapi.TraceID(ctx))
	} else if timedOut {
		message.Status = "failed"
		message.Error = failure("GENERATION_TIMEOUT", httpapi.TraceID(ctx))
	} else if cause != nil || len(tools) > 0 {
		message.Status = "failed"
		code := "TOOL_FAILED"
		if storageFailure {
			code = "SERVICE_UNAVAILABLE"
		} else if errors.Is(cause, ports.ErrUnavailable) {
			code = "LLM_UNAVAILABLE"
		}
		message.Error = failure(code, httpapi.TraceID(ctx))
	}
	message.Outcome = ports.OutcomeUnknown
	if message.Status == "completed" {
		switch result.Outcome {
		case ports.OutcomeAnswered, ports.OutcomeNeedsClarification, ports.OutcomeKnowledgeGap:
			message.Outcome = result.Outcome
		}
	}
	saved, err := s.Store.Finish(cleanup, owner, p.Conversation.ID, message)
	if err != nil {
		return err
	} // Never report a terminal event before durable storage.
	if saved.Status == "interrupted" || ctx.Err() != nil {
		return nil
	}
	event := "message.completed"
	if saved.Status == "failed" {
		event = "message.failed"
	}
	visible := []Message{saved}
	s.checkSources(ctx, visible)
	return sink.Event(event, map[string]Message{"message": visible[0]})
}

func (s Service) checkSources(ctx context.Context, messages []Message) {
	if s.Knowledge == nil {
		return
	}
	for i := range messages {
		m := &messages[i]
		allowed := make([]Source, 0, len(m.Sources))
		for _, src := range m.Sources {
			// Legacy citations predate versioned HR documents.
			if src.Direction != "hr" {
				allowed = append(allowed, src)
				continue
			}
			doc, err := s.Knowledge.Read(ctx, src.DocumentID)
			if err != nil || doc.Version != src.Version {
				m.Text = "Источник этого ответа больше недоступен."
				allowed = allowed[:0]
				break
			}
			allowed = append(allowed, src)
		}
		m.Sources = allowed
	}
}
