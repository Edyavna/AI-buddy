package knowledge

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func (s Uploads) registerHR(mux *http.ServeMux, session auth.Handler) {
	for path, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/hr/documents": s.hrListHTTP, "POST /api/v1/hr/documents": s.hrSaveHTTP,
		"GET /api/v1/hr/documents/{id}": s.hrGetHTTP, "PATCH /api/v1/hr/documents/{id}": s.hrSaveHTTP,
		"POST /api/v1/hr/documents/{id}/{action}": s.hrPublishHTTP,
	} {
		mux.Handle(path, session.Require(handler, false))
	}
}

func hrResult(w http.ResponseWriter, r *http.Request, result any, err error) {
	if errors.Is(err, errDocumentVersion) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "DOCUMENT_CHANGED", Message: "Документ уже изменился"})
		return
	}
	if errors.Is(err, ports.ErrInvalid) {
		httpapi.WriteError(w, r, httpapi.ValidationError(nil))
		return
	}
	if errors.Is(err, ports.ErrConflict) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "IDEMPOTENCY_CONFLICT", Message: "Конфликт операции с документом"})
		return
	}
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, result)
}

func (s Uploads) hrListHTTP(w http.ResponseWriter, r *http.Request) {
	items, err := s.HRList(r.Context())
	hrResult(w, r, struct {
		Items []HRDocument `json:"items"`
	}{items}, err)
}

func (s Uploads) hrGetHTTP(w http.ResponseWriter, r *http.Request) {
	result, err := s.HRGet(r.Context(), r.PathValue("id"))
	hrResult(w, r, result, err)
}

func (s Uploads) hrSaveHTTP(w http.ResponseWriter, r *http.Request) {
	var in HRDocumentInput
	if err := httpapi.DecodeJSONLimit(w, r, &in, 6*MaxMarkdownBytes+4096); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	result, err := s.HRSave(r.Context(), r.PathValue("id"), in)
	hrResult(w, r, result, err)
}

func (s Uploads) hrPublishHTTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	result, err := s.HRPublish(r.Context(), r.PathValue("id"), in.Version, r.PathValue("action"))
	hrResult(w, r, result, err)
}
