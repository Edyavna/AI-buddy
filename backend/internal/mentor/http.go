package mentor

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Handler struct {
	Store     Store
	Questions ports.OpenQuestionCounter
}

func (h Handler) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/mentor/interns", session.Require(http.HandlerFunc(h.list), false))
	mux.Handle("GET /api/v1/mentor/interns/{id}/plan", session.Require(http.HandlerFunc(h.plan), false))
}

func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.List(r.Context(), h.Questions)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, struct {
		Items []InternSummary `json:"items"`
	}{Items: items})
}

func (h Handler) plan(w http.ResponseWriter, r *http.Request) {
	plan, err := h.Store.Plan(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, plan)
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ports.ErrForbidden) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Операция доступна только наставнику"})
		return
	}
	httpapi.WriteServiceError(w, r, err)
}
