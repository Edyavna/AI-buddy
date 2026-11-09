package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

// probe is a consumer supplied by a test module. It exercises the actual route
// registration/decoder/middleware seam without adding a fake production API.
type probe struct {
	agent  ports.Agent
	access ports.AccessChecker
}

func (p probe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Text string `json:"text"`
	}
	if err := httpapi.DecodeJSON(w, r, &input); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	granted, err := p.access.Check(r.Context(), ports.AccessCheck{ResourceID: testkit.ResourceID, Permission: "read"})
	if err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 503, Code: "SERVICE_UNAVAILABLE", Message: "Dependency unavailable"})
		return
	}
	var answer strings.Builder
	_, err = p.agent.Run(r.Context(), ports.AgentInput{ConversationID: "conversation_demo_1", AssistantMessageID: "msg_a1", Text: input.Text}, func(ctx context.Context, event ports.AgentEvent) error {
		if event.Kind == "answer.delta" {
			answer.WriteString(event.Text)
		}
		return nil
	})
	if err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 503, Code: "SERVICE_UNAVAILABLE", Message: "Dependency unavailable"})
		return
	}
	httpapi.WriteJSON(w, 200, struct {
		Answer  string `json:"answer"`
		Granted bool   `json:"granted"`
	}{answer.String(), granted})
}

type grantedProvider struct{ testkit.Access }

func (*grantedProvider) Check(ctx context.Context, _ ports.AccessCheck) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}

var _ ports.AccessProvider = (*grantedProvider)(nil)

func TestModulesCanBeReplacedWithoutChangingHandler(t *testing.T) {
	for _, tt := range []struct {
		name, answer string
		provider     ports.AccessProvider
		granted      bool
	}{
		{"pending adapter", "waiting", &testkit.Access{}, false},
		{"granted adapter", "ready", &grantedProvider{}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			module := probe{agent: testkit.Agent{Events: []ports.AgentEvent{{Kind: "answer.delta", Text: tt.answer}}}, access: tt.provider}
			handler := httpapi.New(httpapi.Options{
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AllowedOrigins: []string{"http://localhost:5173"},
				Register: func(mux *http.ServeMux) { mux.Handle("POST /test-module", module) },
			})
			r := httptest.NewRequest("POST", "/test-module", strings.NewReader(`{"text":"setup"}`))
			// Test-only identity setup. Production identity belongs to session middleware.
			r = r.WithContext(testkit.Context(r.Context()))
			r.Header.Set("Origin", "http://localhost:5173")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			var result struct {
				Answer  string
				Granted bool
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Answer != tt.answer || result.Granted != tt.granted {
				t.Fatalf("wrong injected result: %+v", result)
			}
			if w.Header().Get("X-Trace-ID") == "" {
				t.Fatal("platform middleware was bypassed")
			}
		})
	}
}
