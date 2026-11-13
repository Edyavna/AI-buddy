package knowledge

import (
	"encoding/json"
	"errors"
	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"net/http"
)

func (s Uploads) RegisterResolutions(mux *http.ServeMux, session auth.Handler, g ResolutionGenerator) {
	for _, direction := range []string{"mentor", "hr"} {
		root := "/api/v1/" + direction + "/questions/{question_id}/instruction-draft"
		wrap := func(h http.HandlerFunc) http.Handler {
			return session.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p, err := resolutionPrincipal(r, direction)
				if err != nil {
					writeResolutionError(w, r, err)
					return
				}
				_ = p
				h(w, r)
			}), false)
		}
		mux.Handle("GET "+root, wrap(func(w http.ResponseWriter, r *http.Request) {
			result, err := s.ResolutionWorkspace(r.Context(), r.PathValue("question_id"))
			resolutionReply(w, r, result, err)
		}))
		mux.Handle("GET "+root+"/targets/{target_id}", wrap(func(w http.ResponseWriter, r *http.Request) {
			result, err := s.ResolutionTarget(r.Context(), r.PathValue("question_id"), r.PathValue("target_id"))
			resolutionReply(w, r, result, err)
		}))
		mux.Handle("PUT "+root, wrap(func(w http.ResponseWriter, r *http.Request) {
			in, decodeErr := decodeResolutionInput(w, r)
			if decodeErr != nil {
				httpapi.WriteError(w, r, decodeErr)
				return
			}
			result, err := s.SaveResolution(r.Context(), r.PathValue("question_id"), in)
			resolutionReply(w, r, result, err)
		}))
		mux.Handle("POST "+root+"/generate", wrap(func(w http.ResponseWriter, r *http.Request) {
			result, err := s.SuggestResolution(r.Context(), r.PathValue("question_id"), g)
			resolutionReply(w, r, result, err)
		}))
		for _, action := range []string{"publish", "retry"} {
			mux.Handle("POST "+root+"/"+action, wrap(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Revision int64 `json:"revision"`
					Reviewed bool  `json:"reviewed"`
				}
				if err := httpapi.DecodeJSON(w, r, &in); err != nil {
					httpapi.WriteError(w, r, err)
					return
				}
				result, err := s.PublishResolution(r.Context(), r.PathValue("question_id"), in.Revision, in.Reviewed, action == "retry")
				resolutionReply(w, r, result, err)
			}))
		}
	}
}

func decodeResolutionInput(w http.ResponseWriter, r *http.Request) (ResolutionInput, *httpapi.Error) {
	var fields map[string]json.RawMessage
	if err := httpapi.DecodeJSONLimit(w, r, &fields, 8*MaxMarkdownBytes+16384); err != nil {
		return ResolutionInput{}, err
	}
	if len(fields) != 5 {
		return ResolutionInput{}, httpapi.ValidationError(nil)
	}
	for _, key := range []string{"revision", "title", "content", "target_document_id", "base_version"} {
		if len(fields[key]) == 0 || (key != "target_document_id" && key != "base_version" && string(fields[key]) == "null") {
			return ResolutionInput{}, httpapi.ValidationError(nil)
		}
	}
	raw, _ := json.Marshal(fields)
	var in ResolutionInput
	if json.Unmarshal(raw, &in) != nil {
		return in, httpapi.ValidationError(nil)
	}
	return in, nil
}
func resolutionPrincipal(r *http.Request, direction string) (bool, error) {
	p, err := identity.Require(r.Context())
	if err != nil {
		return false, err
	}
	if string(p.Role) != direction {
		return false, ports.ErrForbidden
	}
	return true, nil
}
func resolutionReply(w http.ResponseWriter, r *http.Request, result any, err error) {
	if err != nil {
		writeResolutionError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, result)
}

func writeResolutionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrConflict):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "INSTRUCTION_CHANGED", Message: "Черновик, исходный вопрос или версия документа изменились. Перечитайте данные перед повтором."})
	case errors.Is(err, ports.ErrForbidden):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Операция недоступна для этой роли"})
	default:
		writeUploadError(w, r, err)
	}
}
