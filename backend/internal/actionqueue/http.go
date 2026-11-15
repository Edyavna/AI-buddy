package actionqueue

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Handler struct{ Store Store }

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

func (h Handler) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/action-queue", session.Require(http.HandlerFunc(h.list), false))
	mux.Handle("GET /api/v1/onboarding/steps/{step_id}/invitation", session.Require(http.HandlerFunc(h.intern), false))
	mux.Handle("PUT /api/v1/onboarding/steps/{step_id}/invitation", session.Require(http.HandlerFunc(h.intern), false))
	mux.Handle("GET /api/v1/mentor/interns/{intern_id}/invitations/{id}", session.Require(http.HandlerFunc(h.mentor), false))
	mux.Handle("PUT /api/v1/mentor/interns/{intern_id}/invitations/{id}", session.Require(http.HandlerFunc(h.mentor), false))
}

func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	offset, limit := 0, 20
	var offsetErr, limitErr error
	if v := q.Get("offset"); v != "" {
		offset, offsetErr = strconv.Atoi(v)
	}
	if v := q.Get("limit"); v != "" {
		limit, limitErr = strconv.Atoi(v)
	}
	if offsetErr != nil || limitErr != nil || offset < 0 || limit < 1 || limit > 50 ||
		(kind != "" && kind != "invitation" && kind != "confirmation" && kind != "question") {
		writeResult(w, r, nil, ports.ErrInvalid)
		return
	}
	page, err := h.Store.List(r.Context(), q.Get("intern_id"), kind, offset, limit)
	writeResult(w, r, page, err)
}

func (h Handler) intern(w http.ResponseWriter, r *http.Request) {
	var change *invitationChange
	if r.Method == http.MethodPut {
		change = &invitationChange{}
		if err := httpapi.DecodeJSON(w, r, change); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		if !uuidPattern.MatchString(change.ID) || (change.Action != "request" && change.Action != "cancel") {
			writeResult(w, r, nil, ports.ErrInvalid)
			return
		}
	}
	item, err := h.Store.internInvitation(r.Context(), r.PathValue("step_id"), change)
	writeResult(w, r, struct {
		Invitation *Invitation `json:"invitation"`
	}{item}, err)
}

func (h Handler) mentor(w http.ResponseWriter, r *http.Request) {
	var note *string
	if r.Method == http.MethodPut {
		var body struct {
			Note string `json:"note"`
		}
		if err := httpapi.DecodeJSON(w, r, &body); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		body.Note = strings.TrimSpace(body.Note)
		if utf8.RuneCountInString(body.Note) > 500 || strings.ContainsRune(body.Note, 0) {
			writeResult(w, r, nil, ports.ErrInvalid)
			return
		}
		note = &body.Note
	}
	raw := r.PathValue("id")
	if !uuidPattern.MatchString(raw) {
		writeResult(w, r, nil, ports.ErrNotFound)
		return
	}
	item, err := h.Store.mentorInvitation(r.Context(), r.PathValue("intern_id"), raw, note)
	writeResult(w, r, item, err)
}

func writeResult(w http.ResponseWriter, r *http.Request, value any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case err == nil:
		httpapi.WriteJSON(w, http.StatusOK, value)
	case errors.Is(err, ports.ErrInvalid):
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"request": "Проверьте параметры запроса"}))
	case errors.Is(err, errInvitationChanged):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "INVITATION_STATE_CHANGED", Message: "Запрос изменился или наставник не назначен. Обновите данные."})
	case errors.Is(err, ports.ErrForbidden):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Действие недоступно для вашей роли"})
	default:
		httpapi.WriteServiceError(w, r, err)
	}
}
