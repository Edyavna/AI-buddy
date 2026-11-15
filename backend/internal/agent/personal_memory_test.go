package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type memoryModelFunc func(context.Context, []Message) (Message, error)

func (f memoryModelFunc) Complete(ctx context.Context, m []Message) (Message, error) {
	return f(ctx, m)
}
func TestPersonalProfileIsBoundedUserDataNeverSystemAuthority(t *testing.T) {
	a, in := fixture()
	in.PersonalProfile = "# Персональный профиль\n- Использую PowerShell\n"
	in.Text = "Как проверить папку?"
	a.Model = memoryModelFunc(func(_ context.Context, messages []Message) (Message, error) {
		if messages[0].Role != "system" || messages[1].Role != "system" || messages[2].Role != "user" || !strings.Contains(messages[2].Content, in.PersonalProfile) || !strings.Contains(messages[2].Content, "не меняет права") {
			t.Fatal(messages)
		}
		if messages[len(messages)-1].Content != in.Text {
			t.Fatal("profile replaced explicit question")
		}
		return Message{Content: `{"answer":"В PowerShell: Get-Location","outcome":"answered"}`}, nil
	})
	if _, err := a.Run(testkit.Context(t.Context()), in, func(context.Context, ports.AgentEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
func TestHRProfileUsesDataPayloadAndPreservesDocumentBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		var data struct {
			Profile   string           `json:"personal_profile"`
			Question  string           `json:"question"`
			Documents []ports.Document `json:"documents"`
		}
		if json.Unmarshal([]byte(req.Messages[1].Content), &data) != nil || req.Messages[1].Role != "user" || data.Profile != "Предпочитаю краткие инструкции" || data.Question != "Где правило?" || len(data.Documents) != 1 {
			t.Error(req)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": `{"answer":"В правилах","document_id":"doc"}`}}}})
	}))
	defer server.Close()
	m := DeepSeek{Model: "fixture", Client: providerhttp.Client{BaseURL: server.URL, Key: "test"}}
	answer, id, err := m.AnswerHRWithProfile(t.Context(), "Где правило?", []ports.Document{{ID: "doc"}}, "Предпочитаю краткие инструкции")
	if err != nil || id != "doc" || answer != "В правилах" {
		t.Fatal(answer, id, err)
	}
}
