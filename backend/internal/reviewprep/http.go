package reviewprep

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Handler struct {
	Store     Store
	Assistant Assistant
}

func (h Handler) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/review-preparations/template", session.Require(http.HandlerFunc(h.template), false))
	mux.Handle("PUT /api/v1/review-preparations/template", session.Require(http.HandlerFunc(h.saveTemplate), false))
	mux.Handle("GET /api/v1/review-preparations", session.Require(http.HandlerFunc(h.latest), true))
	mux.Handle("POST /api/v1/review-preparations", session.Require(http.HandlerFunc(h.create), true))
	mux.Handle("GET /api/v1/review-preparations/{id}", session.Require(http.HandlerFunc(h.get), true))
	mux.Handle("PUT /api/v1/review-preparations/{id}", session.Require(http.HandlerFunc(h.update), true))
	mux.Handle("POST /api/v1/review-preparations/{id}/submit", session.Require(http.HandlerFunc(h.submit), true))
	mux.Handle("POST /api/v1/review-preparations/{id}/assistant", session.Require(http.HandlerFunc(h.assistant), true))
	mux.Handle("POST /api/v1/review-preparations/{id}/refresh", session.Require(http.HandlerFunc(h.refresh), true))
	mux.Handle("GET /api/v1/mentor/review-preparations", session.Require(http.HandlerFunc(h.mentorList), false))
	mux.Handle("GET /api/v1/mentor/review-preparations/{id}", session.Require(http.HandlerFunc(h.mentorGet), false))
}

func (h Handler) template(w http.ResponseWriter, r *http.Request) {
	t, err := h.Store.Template(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, t)
}

func (h Handler) saveTemplate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items    []TemplateItem `json:"items"`
		Revision int64          `json:"revision"`
	}
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if len(body.Items) == 0 {
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"items": "Добавьте хотя бы один пункт"}))
		return
	}
	t, err := h.Store.SaveTemplate(r.Context(), body.Items, body.Revision)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, t)
}

func (h Handler) latest(w http.ResponseWriter, r *http.Request) {
	prep, err := h.Store.Latest(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Item *Preparation `json:"item"`
	}{prep})
}

func (h Handler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PRURL  string  `json:"pr_url"`
		StepID *string `json:"step_id"`
		Manual bool    `json:"manual"`
	}
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if body.PRURL == "" {
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"pr_url": "Укажите ссылку на pull request"}))
		return
	}
	prep, err := h.Store.Create(r.Context(), body.PRURL, body.StepID, body.Manual)
	if errors.Is(err, ports.ErrInvalid) {
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"pr_url": "Поддерживается только адрес вида https://github.com/владелец/репозиторий/pull/номер"}))
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, prep)
}

func (h Handler) get(w http.ResponseWriter, r *http.Request) {
	prep, err := h.Store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, prep)
}

func (h Handler) update(w http.ResponseWriter, r *http.Request) {
	var body Update
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	prep, err := h.Store.Update(r.Context(), r.PathValue("id"), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, prep)
}

func (h Handler) submit(w http.ResponseWriter, r *http.Request) {
	var body Submit
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	prep, err := h.Store.Submit(r.Context(), r.PathValue("id"), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, prep)
}

func (h Handler) assistant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	prep, err := h.Store.Get(ctx, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	template, err := h.Store.Template(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	assistant := h.Assistant
	if assistant == nil {
		assistant = DemoAssistant{}
	}
	result, err := assistant.Help(ctx, prep, template)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

func (h Handler) refresh(w http.ResponseWriter, r *http.Request) {
	prep, changed, err := h.Store.Refresh(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Preparation Preparation `json:"preparation"`
		Changed     bool        `json:"changed"`
	}{prep, changed})
}

func (h Handler) mentorList(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.MentorList(r.Context(), r.URL.Query().Get("intern_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Items []ListItem `json:"items"`
	}{items})
}

func (h Handler) mentorGet(w http.ResponseWriter, r *http.Request) {
	prep, err := h.Store.MentorGet(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, prep)
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
	case errors.Is(err, ErrDraftExists):
		result = &httpapi.Error{Status: 409, Code: "REVIEW_DRAFT_EXISTS", Message: "Черновик подготовки уже существует, обновите его"}
	case errors.Is(err, ErrChanged):
		result = &httpapi.Error{Status: 409, Code: "REVIEW_CHANGED", Message: "Подготовка уже изменилась, обновите страницу"}
	case errors.Is(err, ErrTransition):
		result = &httpapi.Error{Status: 409, Code: "REVIEW_REFRESH_UNAVAILABLE", Message: "Обновление сведений доступно только при интеграции GitHub"}
	case errors.Is(err, ErrPRNotFound):
		result = &httpapi.Error{Status: 404, Code: "PR_NOT_FOUND", Message: "Pull request не найден или репозиторий недоступен"}
	case errors.Is(err, ErrPRRateLimit):
		result = &httpapi.Error{Status: 503, Code: "GITHUB_RATE_LIMIT", Message: "Превышен лимит запросов GitHub, повторите позже"}
	default:
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteError(w, r, result)
}
