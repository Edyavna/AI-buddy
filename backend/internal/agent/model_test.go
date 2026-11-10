package agent

import (
	"context"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeepSeek(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Error(r.URL)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Ответ"},"finish_reason":"stop"}]}`))
	}))
	defer s.Close()
	m := DeepSeek{Client: providerhttp.Client{BaseURL: s.URL, Key: "test"}, Model: "configured-model"}
	out, e := m.Complete(context.Background(), []Message{{Role: "user", Content: "Вопрос"}})
	if e != nil || out.Content != "Ответ" {
		t.Fatal(out, e)
	}
	if len(toolSchemas()) != 6 {
		t.Fatal("tools")
	}
	m.Model = ""
	if _, e = m.Complete(context.Background(), nil); e == nil {
		t.Fatal("empty model")
	}
}
