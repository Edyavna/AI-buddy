package knowledgegaps

import (
	"net/http"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func (h Handler) registerContext(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("POST /api/v1/messages/{id}/context-documents/{document_id}/opened", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version int64 `json:"version"`
		}
		if err := httpapi.DecodeJSON(w, r, &in); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		err := h.Store.RecordContextOpen(r.Context(), r.PathValue("id"), r.PathValue("document_id"), in.Version)
		writeResult(w, r, struct {
			Opened bool `json:"opened"`
		}{err == nil}, err)
	}), true))
	mux.Handle("GET /api/v1/questions/{id}/context-documents/{document_id}", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := h.Store.ContextDocument(r.Context(), r.PathValue("id"), r.PathValue("document_id"))
		writeContextDocument(w, r, value, err)
	}), true))
	mux.Handle("GET /api/v1/messages/{id}/question-context", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := h.Store.ContextPreview(r.Context(), r.PathValue("id"), r.URL.Query().Get("direction"))
		writeResult(w, r, value, err)
	}), true))
	mux.Handle("GET /api/v1/messages/{id}/context-documents/{document_id}", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := h.Store.OpenContextDocument(r.Context(), r.PathValue("id"), r.PathValue("document_id"))
		writeContextDocument(w, r, value, err)
	}), true))
	for _, direction := range []string{"mentor", "hr"} {
		mux.Handle("GET /api/v1/"+direction+"/questions/{id}/context-documents/{document_id}", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, _ := identity.Require(r.Context())
			if string(p.Role) != direction {
				writeError(w, r, ports.ErrForbidden)
				return
			}
			value, err := h.Store.ContextDocument(r.Context(), r.PathValue("id"), r.PathValue("document_id"))
			writeContextDocument(w, r, value, err)
		}), false))
	}
}
func writeContextDocument(w http.ResponseWriter, r *http.Request, d ports.Document, err error) {
	writeResult(w, r, struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Content   string    `json:"content"`
		UpdatedAt time.Time `json:"updated_at"`
		Direction string    `json:"direction"`
		Version   int64     `json:"version"`
	}{d.ID, d.Title, d.Content, d.UpdatedAt.UTC(), d.Direction, d.Version}, err)
}
