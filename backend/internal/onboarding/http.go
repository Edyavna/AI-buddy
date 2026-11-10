package onboarding

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Handler struct{ Service Service }

func (h Handler) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/onboarding/plan", session.Require(http.HandlerFunc(h.get), true))
	mux.Handle("PATCH /api/v1/onboarding/steps/{step_id}", session.Require(http.HandlerFunc(h.patch), true))
}
func (h Handler) get(w http.ResponseWriter, r *http.Request) {
	plan, err := h.Service.Get(r.Context())
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, plan)
}
func (h Handler) patch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status *string `json:"status"`
	}
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if body.Status == nil {
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"status": "Укажите статус"}))
		return
	}
	plan, err := h.Service.Update(r.Context(), r.PathValue("step_id"), *body.Status)
	if errors.Is(err, ErrTransition) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "STEP_TRANSITION_FORBIDDEN", Message: "Обновите план: переход сейчас недоступен"})
		return
	}
	if errors.Is(err, ports.ErrInvalid) {
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"status": "Недопустимый статус"}))
		return
	}
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, plan)
}
