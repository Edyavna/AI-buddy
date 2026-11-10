package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestDecodeJSON(t *testing.T) {
	for _, tt := range []struct {
		name, body, media string
		status            int
	}{
		{"valid", `{"name":"ok"}`, "application/json", 200},
		{"charset", `{"name":"ok"}`, "application/json; charset=utf-8", 200},
		{"unknown", `{"name":"ok","user_id":"forged"}`, "application/json", 422},
		{"type", `{"name":12}`, "application/json", 422},
		{"null", `null`, "application/json", 422},
		{"array", `[]`, "application/json", 422},
		{"malformed", `{"name":`, "application/json", 400},
		{"unknown and malformed", `{"extra":1,`, "application/json", 400},
		{"trailing", `{} {}`, "application/json", 400},
		{"empty", ``, "application/json", 400},
		{"wrong media", `{}`, "text/plain", 400},
		{"too large", `{"name":"` + strings.Repeat("a", 1<<20) + `"}`, "application/json", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := Middleware(quietLogger(), []string{"http://localhost:5173"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Name string `json:"name"`
				}
				if err := DecodeJSON(w, r, &body); err != nil {
					WriteError(w, r, err)
					return
				}
				WriteJSON(w, 200, body)
			}))
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			r.Header.Set("Origin", "http://localhost:5173")
			r.Header.Set("Content-Type", tt.media)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if tt.status >= 400 {
				assertAPIError(t, w)
			}
		})
	}
}

func assertAPIError(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var envelope struct {
		Error Error `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code == "" || envelope.Error.TraceID == "" || envelope.Error.TraceID != w.Header().Get("X-Trace-ID") {
		t.Fatalf("invalid ApiError: %s", w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}

func TestServer(t *testing.T) {
	for _, ready := range []bool{true, false} {
		h := New(Options{Logger: quietLogger(), Readiness: func(context.Context) error {
			if ready {
				return nil
			}
			return errors.New("db secret")
		}})
		for _, path := range []string{"/healthz", "/readyz", "/api/v1/me"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			want := 200
			if path == "/readyz" && !ready {
				want = 503
			}
			if path == "/api/v1/me" {
				want = 404
			}
			if w.Code != want {
				t.Fatalf("%s: %d", path, w.Code)
			}
			if want >= 400 {
				assertAPIError(t, w)
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("leaked database error")
			}
		}
	}
	h := New(Options{Logger: quietLogger()})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestOriginAndValidation(t *testing.T) {
	// An arbitrary caller's root mux handler is an operation, not our 404 fallback.
	root := http.NewServeMux()
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	rootResponse := httptest.NewRecorder()
	Middleware(quietLogger(), nil, root).ServeHTTP(rootResponse, httptest.NewRequest("POST", "/", nil))
	if rootResponse.Code != 403 {
		t.Fatal("root mux bypassed Origin")
	}
	h := New(Options{Logger: quietLogger(), AllowedOrigins: []string{"http://localhost:5173"}, Register: func(mux *http.ServeMux) {
		mux.HandleFunc("POST /api/v1/test", func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, r, ValidationError(map[string]string{"name": "Обязательное поле"}))
		})
	}})
	for _, origin := range []string{"", "null", "https://evil.example", "http://localhost:5173"} {
		r := httptest.NewRequest("POST", "/api/v1/test", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 403
		if origin == "http://localhost:5173" {
			want = 422
		}
		if w.Code != want {
			t.Fatal(w.Code)
		}
		assertAPIError(t, w)
	}
}

func TestRecoveryAndPrivacy(t *testing.T) {
	var logs bytes.Buffer
	h := Middleware(slog.New(slog.NewJSONHandler(&logs, nil)), nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret-password") }))
	r := httptest.NewRequest("GET", "/secret-path?token=secret-token", nil)
	r.Header.Set("Cookie", "buddy_session=secret-cookie")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assertAPIError(t, w)
	if w.Code != 500 || strings.Contains(w.Body.String()+logs.String(), "secret") {
		t.Fatalf("panic leaked: %s %s", w.Body.String(), logs.String())
	}
}

func TestCommittedPanicAbortsWithoutJSON(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "panic", true: "abort"}[abort], func(t *testing.T) {
			h := Middleware(quietLogger(), nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte(": heartbeat\n\n"))
				if abort {
					panic(http.ErrAbortHandler)
				}
				panic("secret")
			}))
			w := httptest.NewRecorder()
			defer func() {
				if recover() != http.ErrAbortHandler {
					t.Error("must abort committed response")
				}
				if strings.Contains(w.Body.String(), "error") {
					t.Error(w.Body.String())
				}
			}()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		})
	}
}

func TestWriteJSONEncodingFailure(t *testing.T) {
	h := Middleware(quietLogger(), nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { WriteJSON(w, 200, make(chan int)) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	assertAPIError(t, w)
}
