package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type savedSession struct {
	user    User
	expires time.Time
}
type memoryStore struct {
	users    map[string]User
	sessions map[string]savedSession
	err      error
}

func (m *memoryStore) Accounts(context.Context) ([]User, error) {
	return []User{m.users["intern"]}, m.err
}
func (m *memoryStore) CreateSession(_ context.Context, id string, hash, old []byte, expires time.Time) (User, error) {
	if m.err != nil {
		return User{}, m.err
	}
	u, ok := m.users[id]
	if !ok {
		return User{}, ports.ErrNotFound
	}
	delete(m.sessions, string(old))
	m.sessions[string(hash)] = savedSession{u, expires}
	return u, nil
}
func (m *memoryStore) Resolve(_ context.Context, hash []byte, now time.Time) (User, error) {
	if m.err != nil {
		return User{}, m.err
	}
	s, ok := m.sessions[string(hash)]
	if !ok || !now.Before(s.expires) {
		return User{}, identity.ErrUnauthenticated
	}
	return s.user, nil
}
func (m *memoryStore) DeleteSession(_ context.Context, hash []byte) error {
	if m.err != nil {
		return m.err
	}
	delete(m.sessions, string(hash))
	return nil
}
func fixture() (*Service, *memoryStore) {
	store := &memoryStore{users: map[string]User{
		"intern": {ID: "intern", Name: "Стажер", Role: identity.Intern, Team: Team{ID: "team", Name: "Команда"}},
		"mentor": {ID: "mentor", Name: "Наставник", Role: identity.Mentor, Team: Team{ID: "team", Name: "Команда"}},
	}, sessions: map[string]savedSession{}}
	return &Service{Store: store, TTL: 24 * time.Hour}, store
}
func request(handler http.Handler, method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func handler(h Handler) http.Handler {
	return httpapi.New(httpapi.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AllowedOrigins: []string{"http://localhost:5173"}, Register: func(mux *http.ServeMux) {
		h.Register(mux)
		mux.Handle("GET /private", h.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := identity.Require(r.Context())
			if err != nil || p.UserID != "intern" {
				panic("missing verified principal")
			}
			w.WriteHeader(200)
		}), true))
	}})
}
func TestSessionLifecycle(t *testing.T) {
	service, store := fixture()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	h := handler(Handler{Service: service, DemoMode: true})
	login := request(h, "POST", "/api/v1/demo/session", `{"account_id":"intern"}`, nil, "http://localhost:5173")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 86400 || cookie.Secure {
		t.Fatalf("cookie: %+v", cookie)
	}
	if _, ok := store.sessions[cookie.Value]; ok {
		t.Fatal("raw token stored")
	}
	for _, path := range []string{"/api/v1/me", "/private"} {
		if w := request(h, "GET", path, "", cookie, ""); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	// A new handler/service with the same store represents a restarted process.
	restarted := handler(Handler{Service: &Service{Store: store, TTL: service.TTL, Now: service.Now}, DemoMode: true})
	if w := request(restarted, "GET", "/api/v1/me", "", cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	swapped := request(h, "POST", "/api/v1/demo/session", `{"account_id":"mentor"}`, cookie, "http://localhost:5173")
	if swapped.Code != 200 {
		t.Fatal(swapped.Body.String())
	}
	mentor := swapped.Result().Cookies()[0]
	if w := request(h, "GET", "/api/v1/me", "", cookie, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/private", "", mentor, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/v1/me", "", mentor, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	now = now.Add(24 * time.Hour)
	if w := request(h, "GET", "/api/v1/me", "", mentor, ""); w.Code != 401 {
		t.Fatal("expired cookie accepted")
	}
	for range 2 {
		w := request(h, "DELETE", "/api/v1/session", "", mentor, "http://localhost:5173")
		if w.Code != 204 || w.Body.Len() != 0 || w.Result().Cookies()[0].MaxAge != -1 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(h, "DELETE", "/api/v1/session", "", nil, "http://localhost:5173"); w.Code != 204 {
		t.Fatal(w.Code)
	}
}
func TestSessionFailuresAndSecureCookie(t *testing.T) {
	service, store := fixture()
	h := handler(Handler{Service: service, DemoMode: true, CookieSecure: true})
	for _, tt := range []struct {
		body string
		code int
	}{
		{`{`, 400}, {`[]`, 422}, {`{}`, 422}, {`{"account_id":null}`, 422}, {`{"account_id":" "}`, 422},
		{`{"account_id":2}`, 422}, {`{"account_id":"intern","user_id":"other"}`, 422}, {`{"account_id":"missing"}`, 404},
	} {
		w := request(h, "POST", "/api/v1/demo/session", tt.body, nil, "http://localhost:5173")
		if w.Code != tt.code || !strings.Contains(w.Body.String(), `"trace_id":"`) {
			t.Fatalf("%s: %d %s", tt.body, w.Code, w.Body.String())
		}
	}
	for _, origin := range []string{"", "https://evil.example"} {
		if w := request(h, "POST", "/api/v1/demo/session", `{"account_id":"intern"}`, nil, origin); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	w := request(h, "POST", "/api/v1/demo/session", `{"account_id":"intern"}`, nil, "http://localhost:5173")
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure {
		t.Fatal("proxy TLS config ignored")
	}
	// Client-controlled forwarded headers cannot enable/disable Secure.
	req := httptest.NewRequest("POST", "https://localhost/api/v1/demo/session", strings.NewReader(`{"account_id":"intern"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	handler(Handler{Service: service, DemoMode: true}).ServeHTTP(rec, req)
	if !rec.Result().Cookies()[0].Secure {
		t.Fatal("direct TLS ignored")
	}
	disabled := handler(Handler{Service: service})
	if w := request(disabled, "GET", "/api/v1/demo/accounts", "", nil, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request(disabled, "POST", "/api/v1/demo/session", `{}`, nil, "http://localhost:5173"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	for _, origin := range []string{"", "https://evil.example"} {
		if w := request(disabled, "POST", "/api/v1/demo/session", `{}`, nil, origin); w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
	if w := request(h, "GET", "/api/v1/demo/accounts", "", nil, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	store.err = ports.ErrUnavailable
	for _, tt := range []struct {
		method, path, body string
		cookie             *http.Cookie
	}{
		{"GET", "/api/v1/demo/accounts", "", nil}, {"POST", "/api/v1/demo/session", `{"account_id":"intern"}`, nil},
		{"GET", "/api/v1/me", "", cookie}, {"DELETE", "/api/v1/session", "", cookie},
	} {
		w := request(h, tt.method, tt.path, tt.body, tt.cookie, "http://localhost:5173")
		if w.Code != 503 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	store.err = errors.New("secret database failure")
	if w := request(h, "GET", "/api/v1/me", "", cookie, ""); w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Body.String())
	}
	if _, err := service.Current(context.Background(), strings.Repeat("x", 257)); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}
