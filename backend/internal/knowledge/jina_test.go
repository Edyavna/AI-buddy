package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"net/http"
	"net/http/httptest"
	"testing"
)

// This checks the protocol without calling Jina or evaluating search quality.
func TestJinaProtocol(t *testing.T) {
	for _, task := range []string{"retrieval.query", "retrieval.passage"} {
		t.Run(task, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/embeddings" || r.Header.Get("Authorization") != "Bearer fake-key" {
					t.Error("incorrect request")
				}
				var request struct {
					Model, Task string
					Dimensions  int
					Input       []string
					Format      string `json:"encoding_format"`
				}
				if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
					t.Error(e)
				}
				if request.Model != "jina-embeddings-v3" || request.Task != task || request.Dimensions != 1024 || request.Format != "float" || len(request.Input) != 1 || request.Input[0] != "Тест" {
					t.Errorf("incorrect payload: %+v", request)
				}
				v := make([]float64, 1024)
				v[0] = 1
				json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": v}}})
			}))
			defer s.Close()
			j := Jina{Client: providerhttp.Client{BaseURL: s.URL, Key: "fake-key"}}
			v, e := j.Embed(context.Background(), task, "Тест")
			if e != nil || len(v) != 1024 || v[0] != 1 {
				t.Fatal(v, e)
			}
		})
	}
}
func TestJinaBatchPreservesInputOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		indices []int
		valid   bool
	}{
		{"out of order", []int{1, 0}, true},
		{"duplicate", []int{0, 0}, false},
		{"out of range", []int{1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct{ Input []string }
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Input) != 2 || req.Input[0] != "Первый" || req.Input[1] != "Второй" {
					t.Errorf("incorrect input: %v, %v", req.Input, err)
				}
				data := make([]map[string]any, 0, len(tc.indices))
				for _, i := range tc.indices {
					v := make([]float64, 1024)
					v[0] = float64(i + 1)
					data = append(data, map[string]any{"index": i, "embedding": v})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			defer s.Close()
			j := Jina{Client: providerhttp.Client{BaseURL: s.URL, Key: "fake"}}
			v, err := j.EmbedBatch(context.Background(), "retrieval.passage", []string{"Первый", "Второй"})
			if !tc.valid {
				if !errors.Is(err, ports.ErrUnavailable) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || len(v) != 2 || v[0][0] != 1 || v[1][0] != 2 {
				t.Fatalf("batch did not preserve input order: %v", err)
			}
		})
	}
}

func TestJinaInvalidResponses(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"data":[{"embedding":[1]}]}`, `broken`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer s.Close()
			j := Jina{Client: providerhttp.Client{BaseURL: s.URL, Key: "fake"}}
			if _, e := j.Embed(context.Background(), "retrieval.query", "Тест"); !errors.Is(e, ports.ErrUnavailable) {
				t.Fatal(e)
			}
		})
	}
	j := Jina{}
	if _, e := j.Embed(context.Background(), "unknown", "Тест"); !errors.Is(e, ports.ErrInvalid) {
		t.Fatal(e)
	}
}
