package knowledge

import (
	"errors"
	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"net/http"
)

func (s Uploads) RegisterGroups(mux *http.ServeMux, session auth.Handler) {
	for _, direction := range []string{"mentor", "hr"} {
		wrap := func(h http.HandlerFunc) http.Handler {
			return session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := resolutionPrincipal(r, direction); err != nil {
					writeResolutionError(w, r, err)
					return
				}
				h(w, r)
			}), false)
		}
		root := "/api/v1/" + direction + "/question-groups"
		mux.Handle("GET "+root, wrap(func(w http.ResponseWriter, r *http.Request) {
			data, err := s.GroupWorkspace(r.Context())
			groupReply(w, r, data, err)
		}))
		save := wrap(func(w http.ResponseWriter, r *http.Request) {
			var in GroupInput
			if err := httpapi.DecodeJSON(w, r, &in); err != nil {
				httpapi.WriteError(w, r, err)
				return
			}
			data, err := s.SaveGroup(r.Context(), r.PathValue("id"), in)
			groupReply(w, r, data, err)
		})
		mux.Handle("POST "+root, save)
		mux.Handle("PUT "+root+"/{id}", save)
		mux.Handle("GET "+root+"/{id}", wrap(func(w http.ResponseWriter, r *http.Request) {
			data, err := s.GetGroup(r.Context(), r.PathValue("id"))
			groupReply(w, r, data, err)
		}))
		for _, action := range []string{"deliver", "close"} {
			mux.Handle("POST "+root+"/{id}/"+action, wrap(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Revision int64 `json:"revision"`
				}
				if err := httpapi.DecodeJSON(w, r, &in); err != nil {
					httpapi.WriteError(w, r, err)
					return
				}
				if in.Revision < 1 {
					resolutionReply(w, r, nil, ports.ErrInvalid)
					return
				}
				data, err := s.DeliverGroup(r.Context(), r.PathValue("id"), in.Revision, action == "close")
				groupReply(w, r, data, err)
			}))
		}
	}
}

func groupReply(w http.ResponseWriter, r *http.Request, data any, err error) {
	if errors.Is(err, ports.ErrConflict) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "GROUP_CHANGED", Message: "Состав, состояние вопросов или версия группы изменились. Перечитайте данные; вопрос может уже участвовать в другой группе."})
		return
	}
	resolutionReply(w, r, data, err)
}
