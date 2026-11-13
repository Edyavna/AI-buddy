package reviewprep_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/reviewprep"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type client struct {
	handler http.Handler
	t       *testing.T
}

func newClient(t *testing.T, store reviewprep.Store) *client {
	t.Helper()
	db := store.DB
	session := auth.Handler{Service: &auth.Service{Store: auth.SQLStore{DB: db}, TTL: 24 * time.Hour}, DemoMode: true}
	reviews := reviewprep.Handler{Store: store}
	return &client{t: t, handler: httpapi.New(httpapi.Options{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		AllowedOrigins: []string{"http://localhost:5173"},
		Register: func(mux *http.ServeMux) {
			session.Register(mux)
			reviews.Register(mux, session)
		},
	})}
}

func (c *client) do(method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, req)
	return w
}

func (c *client) login(account string) *http.Cookie {
	c.t.Helper()
	w := c.do("POST", "/api/v1/demo/session", `{"account_id":"`+account+`"}`, nil, "http://localhost:5173")
	if w.Code != 200 {
		c.t.Fatalf("login %s: %d %s", account, w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func TestReviewPreparationHTTPFlow(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	api := newClient(t, reviewprep.Store{DB: db})
	intern := api.login("intern_demo_1")

	if w := api.do("GET", "/api/v1/review-preparations/template", "", intern, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Наставник тоже читает шаблон своей команды, но не создаёт подготовку.
	mentor := api.login("mentor_demo_1")
	if w := api.do("GET", "/api/v1/review-preparations/template", "", mentor, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := api.do("POST", "/api/v1/review-preparations", `{"pr_url":"`+goodURL+`","manual":true}`, mentor, "http://localhost:5173"); w.Code != 403 {
		t.Fatalf("mentor create must be 403, got %d", w.Code)
	}

	w := api.do("POST", "/api/v1/review-preparations", `{"pr_url":"https://gitlab.com/o/r/pull/1"}`, intern, "http://localhost:5173")
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"field_errors"`) {
		t.Fatalf("unsupported url must be 422, got %d %s", w.Code, w.Body.String())
	}
	w = api.do("POST", "/api/v1/review-preparations", `{"pr_url":"`+goodURL+`","manual":true}`, intern, "http://localhost:5173")
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var prep reviewprep.Preparation
	if err := json.Unmarshal(w.Body.Bytes(), &prep); err != nil {
		t.Fatal(err)
	}
	if prep.PR.Source != "manual" {
		t.Fatalf("expected manual source, got %+v", prep.PR)
	}
	// Повторный клик создания защищён: черновик уже есть.
	if w := api.do("POST", "/api/v1/review-preparations", `{"pr_url":"`+goodURL+`","manual":true}`, intern, "http://localhost:5173"); w.Code != 409 {
		t.Fatalf("duplicate draft must be 409, got %d", w.Code)
	}

	update := `{"revision":1,"description":"Описание","answers":{"summary":"Сделано","verification":"Локально","items":{"task_done":{"done":true}}},"checks":[{"name":"go build","status":"passed","detail":"локально"}]}`
	if w := api.do("PUT", "/api/v1/review-preparations/"+prep.ID, update, intern, "http://localhost:5173"); w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if w := api.do("POST", "/api/v1/review-preparations/"+prep.ID+"/assistant", "", intern, "http://localhost:5173"); w.Code != 200 {
		t.Fatalf("assistant: %d %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), "Стоит дополнить") {
		t.Fatalf("assistant must report missing items: %s", w.Body.String())
	}

	if w := api.do("POST", "/api/v1/review-preparations/"+prep.ID+"/submit", `{"revision":1}`, intern, "http://localhost:5173"); w.Code != 200 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	// Повторная отправка со старой ревизией защищена конфликтом.
	if w := api.do("POST", "/api/v1/review-preparations/"+prep.ID+"/submit", `{"revision":1}`, intern, "http://localhost:5173"); w.Code != 409 {
		t.Fatalf("repeat submit must be 409, got %d", w.Code)
	}

	w = api.do("GET", "/api/v1/mentor/review-preparations", "", mentor, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Сделано") {
		t.Fatalf("mentor list: %d %s", w.Code, w.Body.String())
	}
	w = api.do("GET", "/api/v1/mentor/review-preparations/"+prep.ID, "", mentor, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"history":[{"revision":1`) {
		t.Fatalf("mentor detail: %d %s", w.Code, w.Body.String())
	}
	// Ручной режим: обновление сведений из GitHub недоступно.
	if w := api.do("POST", "/api/v1/review-preparations/"+prep.ID+"/refresh", "", intern, "http://localhost:5173"); w.Code != 409 {
		t.Fatalf("manual refresh must be 409, got %d %s", w.Code, w.Body.String())
	}
	if w := api.do("GET", "/api/v1/review-preparations/unknown", "", intern, ""); w.Code != 404 {
		t.Fatalf("unknown prep must be 404, got %d", w.Code)
	}
	// Смена аккаунта: сессия стажёра не работает после входа наставника с тем же браузером
	// проверяется auth; здесь — что чужой стажёр не видит подготовку.
	other := api.login("intern_demo_2")
	if w := api.do("GET", "/api/v1/review-preparations/"+prep.ID, "", other, ""); w.Code != 404 {
		t.Fatalf("other intern must get 404, got %d", w.Code)
	}
}

func TestReviewPreparationOriginAndBodyGuards(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	api := newClient(t, reviewprep.Store{DB: db})
	intern := api.login("intern_demo_1")
	// State-changing routes without the allowed Origin are rejected by the platform layer.
	if w := api.do("POST", "/api/v1/review-preparations", `{"pr_url":"`+goodURL+`"}`, intern, ""); w.Code != 403 {
		t.Fatalf("missing origin must be 403, got %d", w.Code)
	}
	w := api.do("POST", "/api/v1/review-preparations", `{"pr_url":"`+goodURL+`","manual":true}`, intern, "http://localhost:5173")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var prep reviewprep.Preparation
	if err := json.Unmarshal(w.Body.Bytes(), &prep); err != nil {
		t.Fatal(err)
	}
	for body, code := range map[string]int{
		`{`:              400,
		`[]`:             422,
		`{}`:             422,
		`{"revision":1}`: 200, // пустое обновление допустимо и идемпотентно
	} {
		w := api.do("PUT", "/api/v1/review-preparations/"+prep.ID, body, intern, "http://localhost:5173")
		if w.Code != code {
			t.Fatalf("PUT %s: got %d want %d (%s)", body, w.Code, code, w.Body.String())
		}
	}
	if w := api.do("POST", "/api/v1/review-preparations/"+prep.ID+"/submit", `{"revision":0}`, intern, "http://localhost:5173"); w.Code != 422 {
		t.Fatalf("invalid submit revision must be 422, got %d", w.Code)
	}
}
