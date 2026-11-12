package mentor

import (
	"io"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
)

func (h Handler) available(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.Available(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, struct {
		Items []AvailableIntern `json:"items"`
	}{items})
}

func (h Handler) assign(w http.ResponseWriter, r *http.Request) {
	if _, err := mentorPrincipal(r.Context()); err != nil {
		writeError(w, r, err)
		return
	}
	// Read at most one byte: any body (including whitespace or {}) is forbidden.
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"body": "Запрос назначения не принимает тело"}))
			return
		}
	}
	result, err := h.Store.Assign(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, result)
}
