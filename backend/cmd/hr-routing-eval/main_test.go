package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
)

func TestEvaluationReportsConfusionAndAcceptance(t *testing.T) {
	const path = "../../../demo/hr-routing-eval.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data []example
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	labels := map[string]example{}
	for _, item := range data {
		labels[item.Text] = item
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "pass", true: "fail"}[fail], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages []agent.Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				item := labels[input.Messages[1].Content]
				if fail && item.Category == "hr" {
					item.Direction = "mentor"
				}
				var clarification *string
				if item.Direction == "needs_clarification" {
					clarification = new("Уточните тему")
				}
				content, _ := json.Marshal(map[string]any{"direction": item.Direction, "reason": "Fixture", "split": item.Split, "clarification": clarification})
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": agent.Message{Content: string(content)}}}})
			}))
			defer server.Close()
			model := &agent.DeepSeek{Model: "fixture", Client: providerhttp.Client{BaseURL: server.URL, Key: "fixture"}}
			var out bytes.Buffer
			if err := run(path, model, &out); (err != nil) != fail {
				t.Fatal(err)
			}
			var report struct {
				Passed     bool
				HRToMentor int                `json:"hr_to_mentor"`
				Categories map[string]metrics `json:"by_category"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Passed == fail || len(report.Categories) != 4 {
				t.Fatal(report, err)
			}
			if fail && report.HRToMentor != 40 {
				t.Fatal(report)
			}
			if !fail && report.Categories["mixed"].Recall != 1 {
				t.Fatal(report)
			}
		})
	}
}

func TestEvaluationInputErrors(t *testing.T) {
	var out bytes.Buffer
	model := &agent.DeepSeek{Model: "fixture", Client: providerhttp.Client{BaseURL: "http://127.0.0.1:1", Key: "fixture"}}
	if err := run("missing", &agent.DeepSeek{}, &out); err == nil {
		t.Fatal("missing config accepted")
	}
	path := filepath.Join(t.TempDir(), "dataset.json")
	if err := run(path, model, &out); err == nil {
		t.Fatal("missing dataset accepted")
	}
	for _, value := range []string{"malformed", "[]"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := run(path, model, &out); err == nil {
			t.Fatal("invalid dataset accepted")
		}
	}
}
