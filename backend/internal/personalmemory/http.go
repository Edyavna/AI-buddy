package personalmemory

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func write(w http.ResponseWriter, r *http.Request, value any, err error) {
	if errors.Is(err, ErrDeleted) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 404, Code: "MEMORY_ENTRY_DELETED", Message: "Эта запись удалена из памяти."})
		return
	}
	if errors.Is(err, ports.ErrInvalid) {
		httpapi.WriteError(w, r, httpapi.ValidationError(nil))
		return
	}
	if errors.Is(err, ErrChanged) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "MEMORY_CHANGED", Message: "Память изменилась. Перечитайте записи и подтвердите новое действие."})
		return
	}
	if errors.Is(err, ErrRejected) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 422, Code: "MEMORY_FACT_REJECTED", Message: "Можно сохранять только явно сообщённые рабочие предпочтения из доступных категорий."})
		return
	}
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, value)
}
func (s Store) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/memory", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { v, e := s.Workspace(r.Context()); write(w, r, v, e) }), true))
	for _, method := range []string{"GET", "POST"} {
		mux.Handle(method+" /api/v1/messages/{id}/memory-proposals", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var v Proposals
			var e error
			if r.Method == "POST" {
				var in struct{}
				if err := httpapi.DecodeJSON(w, r, &in); err != nil {
					httpapi.WriteError(w, r, err)
					return
				}
				v, e = s.Suggest(r.Context(), r.PathValue("id"))
			} else {
				v, e = s.Proposals(r.Context(), r.PathValue("id"))
			}
			write(w, r, v, e)
		}), true))
	}
	mux.Handle("PUT /api/v1/memory/proposals/{id}", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in Decision
		if err := httpapi.DecodeJSON(w, r, &in); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		v, e := s.Decide(r.Context(), r.PathValue("id"), in)
		write(w, r, v, e)
	}), true))
	mux.Handle("GET /api/v1/memory/entries/{id}", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.GetEntry(r.Context(), r.PathValue("id"))
		write(w, r, v, e)
	}), true))
	mux.Handle("PUT /api/v1/memory/entries/{id}", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Value    string `json:"value"`
			Revision int64  `json:"revision"`
		}
		if err := httpapi.DecodeJSON(w, r, &in); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		v, e := s.Edit(r.Context(), r.PathValue("id"), in.Value, in.Revision)
		write(w, r, v, e)
	}), true))
	mux.Handle("DELETE /api/v1/memory/entries/{id}", session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int64 `json:"revision"`
		}
		if err := httpapi.DecodeJSON(w, r, &in); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		e := s.Delete(r.Context(), r.PathValue("id"), in.Revision)
		write(w, r, struct {
			Deleted bool `json:"deleted"`
		}{e == nil}, e)
	}), true))
}
