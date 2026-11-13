package onboarding

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type CheckpointHandler struct{ Service Checkpoints }

func (h CheckpointHandler) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("PUT /api/v1/onboarding/steps/{step_id}/confirmation", session.Require(h.set(identity.Intern), true))
	mux.Handle("PUT /api/v1/mentor/interns/{id}/steps/{step_id}/confirmation", session.Require(h.set(identity.Mentor), false))
}

func (h CheckpointHandler) set(role identity.Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Confirmed *bool  `json:"confirmed"`
			Revision  *int64 `json:"revision"`
		}
		if err := httpapi.DecodeJSON(w, r, &body); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		fields := make(map[string]string)
		if body.Confirmed == nil {
			fields["confirmed"] = "Укажите подтверждение"
		}
		if body.Revision == nil || *body.Revision < 1 || *body.Revision > 9007199254740991 {
			fields["revision"] = "Укажите версию шага из плана"
		}
		if len(fields) != 0 {
			httpapi.WriteError(w, r, httpapi.ValidationError(fields))
			return
		}
		change := SetConfirmation{Confirmed: *body.Confirmed, Revision: *body.Revision}
		plan, err := h.Service.Set(r.Context(), role, r.PathValue("id"), r.PathValue("step_id"), change)
		switch {
		case errors.Is(err, ErrPlanChanged):
			httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "CHECKPOINT_REVISION_CONFLICT", Message: "План изменился. Обновите его и проверьте отметки"})
		case errors.Is(err, ErrTransition):
			writeTransitionError(w, r)
		case errors.Is(err, ports.ErrForbidden):
			httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Вы не можете изменить эту отметку"})
		case err != nil:
			httpapi.WriteServiceError(w, r, err)
		default:
			httpapi.WriteJSON(w, 200, plan)
		}
	}
}
