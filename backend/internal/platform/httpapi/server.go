package httpapi

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"
)

type traceKey struct{}

func TraceID(ctx context.Context) string { id, _ := ctx.Value(traceKey{}).(string); return id }

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseWriter) Flush() { _ = w.FlushError() }

// Middleware must remain streaming-friendly: never buffer HTTP response bodies.
func Middleware(logger *slog.Logger, origins []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, origin := range origins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracked := &responseWriter{ResponseWriter: w}
		w = tracked
		id := rand.Text()
		r = r.WithContext(context.WithValue(r.Context(), traceKey{}, id))
		w.Header().Set("X-Trace-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		started := time.Now()
		defer func() {
			// Deliberately omit body, URL, cookie, header and panic contents.
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.Error("request panic", "trace_id", id)
				// After SSE starts no JSON may be appended; close the connection.
				if tracked.status != 0 {
					panic(http.ErrAbortHandler)
				}
				WriteError(w, r, &Error{Status: 500, Code: "INTERNAL_ERROR", Message: "Внутренняя ошибка"})
			}
			logger.Info("request finished", "trace_id", id, "method", r.Method, "status", tracked.status, "duration_ms", time.Since(started).Milliseconds())
		}()
		switch r.Method {
		case http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut:
			if !allowed[r.Header.Get("Origin")] {
				WriteError(w, r, &Error{Status: 403, Code: "ORIGIN_FORBIDDEN", Message: "Недопустимый Origin"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type Options struct {
	Logger         *slog.Logger
	AllowedOrigins []string
	Readiness      func(context.Context) error
	// Register attaches feature handlers at the composition root; never model code.
	Register func(*http.ServeMux)
}

func New(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if opts.Readiness == nil || opts.Readiness(ctx) != nil {
			WriteError(w, r, &Error{Status: 503, Code: "SERVICE_UNAVAILABLE", Message: "База данных недоступна или отключена"})
			return
		}
		WriteJSON(w, 200, map[string]string{"status": "ready"})
	})
	if opts.Register != nil {
		opts.Register(mux)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, &Error{Status: 404, Code: "NOT_FOUND", Message: "Маршрут отсутствует или пока не реализован"})
	})
	return Middleware(opts.Logger, opts.AllowedOrigins, mux)
}
