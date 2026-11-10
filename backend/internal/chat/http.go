package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Handler struct {
	Service Service
	Options StreamOptions
	Logger  *slog.Logger
}

func (h Handler) Register(mux *http.ServeMux, session auth.Handler) {
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/conversations": h.list, "POST /api/v1/conversations": h.create,
		"GET /api/v1/conversations/{conversation_id}":           h.get,
		"GET /api/v1/conversations/{conversation_id}/messages":  h.messages,
		"POST /api/v1/conversations/{conversation_id}/messages": h.send,
	} {
		mux.Handle(pattern, session.Require(handler, true))
	}
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := ""
	switch {
	case errors.Is(err, ErrDuplicate):
		code = "MESSAGE_ALREADY_EXISTS"
	case errors.Is(err, ErrBusy):
		code = "GENERATION_IN_PROGRESS"
	case errors.Is(err, ports.ErrConflict):
		code = "IDEMPOTENCY_CONFLICT"
	}
	if code != "" {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: code, Message: "Обновите историю диалога"})
		return
	}
	if errors.Is(err, ports.ErrInvalid) {
		httpapi.WriteError(w, r, httpapi.ValidationError(nil))
		return
	}
	httpapi.WriteServiceError(w, r, err)
}
func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.List(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, result)
}
func (h Handler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID *string `json:"client_conversation_id"`
		StepID   *string `json:"step_id"`
	}
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if body.ClientID == nil {
		writeError(w, r, ports.ErrInvalid)
		return
	}
	result, created, err := h.Service.Create(r.Context(), *body.ClientID, body.StepID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	status := 200
	if created {
		status = 201
	}
	httpapi.WriteJSON(w, status, result)
}
func (h Handler) get(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.Get(r.Context(), r.PathValue("conversation_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, result)
}
func (h Handler) messages(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.Messages(r.Context(), r.PathValue("conversation_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, result)
}
func (h Handler) send(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("conversation_id")
	if _, err := h.Service.Get(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		ClientID *string `json:"client_message_id"`
		Text     *string `json:"text"`
	}
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if body.ClientID == nil || body.Text == nil {
		writeError(w, r, ports.ErrInvalid)
		return
	}
	prepared, err := h.Service.Prepare(r.Context(), id, *body.ClientID, *body.Text)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	// Stream itself persists every terminal result, including errors opening SSE.
	if err := h.Service.Stream(r.Context(), prepared, httpSink{w: w, timeout: h.Options.defaults().WriteTimeout}, h.Options); err != nil {
		logger := h.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("chat stream could not finish", "trace_id", httpapi.TraceID(r.Context()))
	}
}

type httpSink struct {
	w       http.ResponseWriter
	timeout time.Duration
}

func (s httpSink) write(text string) error {
	controller := http.NewResponseController(s.w)
	if err := controller.SetWriteDeadline(time.Now().Add(s.timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if _, err := fmt.Fprint(s.w, text); err != nil {
		return err
	}
	return controller.Flush()
}
func (s httpSink) Event(kind string, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.write("event: " + kind + "\ndata: " + string(encoded) + "\n\n")
}
func (s httpSink) Heartbeat() error { return s.write(": heartbeat\n\n") }
