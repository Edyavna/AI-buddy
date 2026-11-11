package knowledgegaps

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Handler struct{ Store Store }

func (h Handler) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/messages/{id}/knowledge-gap", session.Require(http.HandlerFunc(h.intern), true))
	mux.Handle("POST /api/v1/messages/{id}/knowledge-gap", session.Require(http.HandlerFunc(h.intern), true))
	mux.Handle("GET /api/v1/mentor/questions", session.Require(http.HandlerFunc(h.list), false))
	mux.Handle("GET /api/v1/mentor/questions/{id}", session.Require(http.HandlerFunc(h.detail), false))
	mux.Handle("PATCH /api/v1/mentor/questions/{id}", session.Require(http.HandlerFunc(h.detail), false))
}

func (h Handler) intern(w http.ResponseWriter, r *http.Request) {
	create := r.Method == http.MethodPost
	q, created, err := h.Store.Intern(r.Context(), r.PathValue("id"), create)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !create {
		httpapi.WriteJSON(w, http.StatusOK, struct {
			Question *Status `json:"question"`
		}{q})
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpapi.WriteJSON(w, status, q)
}

func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	q, err := h.Store.List(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Items []Question `json:"items"`
	}{q})
}

func (h Handler) detail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var q Question
	var err error
	if r.Method == http.MethodPatch {
		var in Update
		if decodeErr := httpapi.DecodeJSON(w, r, &in); decodeErr != nil {
			httpapi.WriteError(w, r, decodeErr)
			return
		}
		q, err = h.Store.Patch(r.Context(), id, in)
	} else {
		q, err = h.Store.Get(r.Context(), id)
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, q)
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var result *httpapi.Error
	switch {
	case errors.Is(err, ports.ErrInvalid):
		result = httpapi.ValidationError(nil)
	case errors.Is(err, ports.ErrForbidden):
		result = &httpapi.Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Действие недоступно для этой роли"}
	case errors.Is(err, ports.ErrMentorUnassigned):
		result = &httpapi.Error{Status: 409, Code: "MENTOR_UNASSIGNED", Message: "Стажёру ещё не назначен наставник"}
	case errors.Is(err, ErrChanged):
		result = &httpapi.Error{Status: 409, Code: "QUESTION_CHANGED", Message: "Вопрос уже изменился"}
	case errors.Is(err, ErrDocument):
		result = &httpapi.Error{Status: 409, Code: "DOCUMENT_NOT_READY", Message: "Для решения добавьте комментарий и доступный готовый документ"}
	case errors.Is(err, ErrOutcome):
		result = &httpapi.Error{Status: 409, Code: "MESSAGE_NOT_ESCALATABLE", Message: "Этот ответ нельзя передать наставнику"}
	case errors.Is(err, ErrTransition):
		result = &httpapi.Error{Status: 409, Code: "QUESTION_TRANSITION_FORBIDDEN", Message: "Этот переход статуса недоступен"}
	default:
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteError(w, r, result)
}
