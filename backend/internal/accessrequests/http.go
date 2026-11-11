package accessrequests

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Handler struct {
	Store   Store
	Checker ports.AccessChecker
}

func (h Handler) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/access-requests", session.Require(http.HandlerFunc(h.list), true))
	mux.Handle("GET /api/v1/access-requests/{request_id}", session.Require(http.HandlerFunc(h.get), true))
	mux.Handle("POST /api/v1/access-requests/{request_id}/confirm", session.Require(http.HandlerFunc(h.confirm), true))
}

func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	requests, err := h.Store.List(r.Context())
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	result := make([]httpapi.AccessRequest, 0, len(requests))
	for _, request := range requests {
		result = append(result, httpapi.RequestDTO(request))
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

func (h Handler) get(w http.ResponseWriter, r *http.Request) {
	request, err := h.Store.Get(r.Context(), r.PathValue("request_id"))
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, httpapi.RequestDTO(request))
}

func (h Handler) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version int64 `json:"draft_version"`
	}
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if body.Version < 1 {
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"draft_version": "Укажите версию черновика"}))
		return
	}
	request, err := h.Store.Confirm(r.Context(), r.PathValue("request_id"), body.Version, h.Checker)
	switch {
	case errors.Is(err, ErrDraftChanged):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "DRAFT_CHANGED", Message: "Черновик изменился"})
	case errors.Is(err, ports.ErrConflict):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "REQUEST_CONFLICT", Message: "Для этого доступа уже есть заявка или доступ выдан"})
	case errors.Is(err, ports.ErrInvalid):
		httpapi.WriteError(w, r, httpapi.ValidationError(nil))
	case err != nil:
		httpapi.WriteServiceError(w, r, err)
	case request.Status == "submitting":
		httpapi.WriteJSON(w, http.StatusAccepted, httpapi.RequestDTO(request))
	default:
		httpapi.WriteJSON(w, http.StatusOK, httpapi.RequestDTO(request))
	}
}
