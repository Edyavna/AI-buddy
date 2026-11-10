package auth

import (
	"net/http"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
)

const CookieName = "buddy_session"

type Handler struct {
	Service  *Service
	DemoMode bool
	// Explicit setting for TLS terminated at a trusted proxy. Forwarded headers
	// from arbitrary clients are deliberately not used to decide cookie security.
	CookieSecure bool
}

func token(r *http.Request) string {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
func (h Handler) Register(mux *http.ServeMux) {
	if h.DemoMode {
		mux.HandleFunc("GET /api/v1/demo/accounts", h.accounts)
		mux.HandleFunc("POST /api/v1/demo/session", h.login)
	}
	mux.HandleFunc("GET /api/v1/me", h.me)
	mux.HandleFunc("DELETE /api/v1/session", h.logout)
}

// Require is reusable by chat/document/request handlers. Services also check
// Principal, so direct Go calls cannot bypass role or ownership checks.
func (h Handler) Require(next http.Handler, internOnly bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := h.Service.Current(r.Context(), token(r))
		if err != nil {
			httpapi.WriteServiceError(w, r, err)
			return
		}
		if internOnly && user.Role != identity.Intern {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Операция доступна только стажеру"})
			return
		}
		p := identity.Principal{UserID: user.ID, TeamID: user.Team.ID, Role: user.Role}
		next.ServeHTTP(w, r.WithContext(identity.WithPrincipal(r.Context(), p)))
	})
}
func (h Handler) accounts(w http.ResponseWriter, r *http.Request) {
	users, err := h.Service.Store.Accounts(r.Context())
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, users)
}
func (h Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID *string `json:"account_id"`
	}
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if body.AccountID == nil || strings.TrimSpace(*body.AccountID) == "" {
		httpapi.WriteError(w, r, httpapi.ValidationError(map[string]string{"account_id": "Выберите пользователя"}))
		return
	}
	user, value, expires, err := h.Service.Login(r.Context(), *body.AccountID, token(r))
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.CookieSecure || r.TLS != nil, Expires: expires, MaxAge: int(h.Service.TTL.Seconds())})
	httpapi.WriteJSON(w, 200, user)
}
func (h Handler) me(w http.ResponseWriter, r *http.Request) {
	user, err := h.Service.Current(r.Context(), token(r))
	if err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, user)
}
func (h Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Logout(r.Context(), token(r)); err != nil {
		httpapi.WriteServiceError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.CookieSecure || r.TLS != nil, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}
