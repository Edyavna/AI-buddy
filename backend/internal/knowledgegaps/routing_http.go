package knowledgegaps

import (
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
)

func (h Handler) registerRouting(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/messages/{id}/questions", session.Require(http.HandlerFunc(h.messageQuestions), true))
	mux.Handle("POST /api/v1/messages/{id}/questions", session.Require(http.HandlerFunc(h.createRouted), true))
	mux.Handle("PUT /api/v1/messages/{id}/question-drafts/{direction}", session.Require(http.HandlerFunc(h.draft), true))
	mux.Handle("GET /api/v1/questions/{id}", session.Require(http.HandlerFunc(h.history), true))
	mux.Handle("POST /api/v1/questions/{id}/reroute", session.Require(http.HandlerFunc(h.reroute), true))
	mux.Handle("GET /api/v1/hr/questions", session.Require(http.HandlerFunc(h.hrList), false))
	mux.Handle("GET /api/v1/hr/questions/{id}", session.Require(http.HandlerFunc(h.hrDetail), false))
	mux.Handle("PATCH /api/v1/hr/questions/{id}", session.Require(http.HandlerFunc(h.hrDetail), false))
}

func writeResult(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
}

func (h Handler) messageQuestions(w http.ResponseWriter, r *http.Request) {
	result, err := h.Store.MessageQuestions(r.Context(), r.PathValue("id"))
	writeResult(w, r, result, err)
}

func (h Handler) createRouted(w http.ResponseWriter, r *http.Request) {
	var in Operation
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	result, err := h.Store.Create(r.Context(), r.PathValue("id"), in)
	writeResult(w, r, result, err)
}

func (h Handler) draft(w http.ResponseWriter, r *http.Request) {
	var in Draft
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	in.Direction = r.PathValue("direction")
	result, err := h.Store.SaveDraft(r.Context(), r.PathValue("id"), in)
	writeResult(w, r, result, err)
}

func (h Handler) history(w http.ResponseWriter, r *http.Request) {
	result, err := h.Store.History(r.Context(), r.PathValue("id"))
	writeResult(w, r, result, err)
}

func (h Handler) reroute(w http.ResponseWriter, r *http.Request) {
	var in Operation
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	result, err := h.Store.Reroute(r.Context(), r.PathValue("id"), in)
	writeResult(w, r, result, err)
}

func (h Handler) hrList(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query()
	result, err := h.Store.HRList(r.Context(), filter.Get("team_id"), filter.Get("status"), filter.Get("q"))
	writeResult(w, r, struct {
		Items []Question `json:"items"`
	}{result}, err)
}

func (h Handler) hrDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		result, err := h.Store.HRGet(r.Context(), r.PathValue("id"))
		writeResult(w, r, result, err)
		return
	}
	var in HRUpdate
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	result, err := h.Store.HRPatch(r.Context(), r.PathValue("id"), in)
	writeResult(w, r, result, err)
}
