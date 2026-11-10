package agent

import (
	"context"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"os"
)

type DeepSeek struct {
	Client providerhttp.Client
	Model  string
}

func FromEnv() *DeepSeek {
	return &DeepSeek{Client: providerhttp.Client{BaseURL: os.Getenv("LLM_BASE_URL"), Key: os.Getenv("LLM_API_KEY")}, Model: os.Getenv("LLM_MODEL")}
}
func toolSchemas() []any {
	names := []string{"search_knowledge", "read_document", "get_my_tasks", "get_access_status", "prepare_access_request", "get_request_status"}
	fields := [][]string{{"query"}, {"document_id"}, {}, {"resource_id", "permission"}, {"resource_id", "permission", "reason"}, {"request_id"}}
	out := []any{}
	for i, name := range names {
		props := map[string]any{}
		for _, f := range fields[i] {
			props[f] = map[string]any{"type": "string"}
		}
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": name, "description": labels[name], "parameters": map[string]any{"type": "object", "properties": props, "required": fields[i], "additionalProperties": false}}})
	}
	return out
}
func (m *DeepSeek) Complete(ctx context.Context, msg []Message) (Message, error) {
	var response struct {
		Choices []struct {
			Message Message `json:"message"`
			Finish  string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if m.Model == "" {
		return Message{}, ports.ErrUnavailable
	}
	e := m.Client.Post(ctx, "/chat/completions", map[string]any{"model": m.Model, "messages": msg, "tools": toolSchemas(), "stream": false, "max_tokens": 4096, "thinking": map[string]string{"type": "disabled"}}, &response)
	if e != nil {
		return Message{}, e
	}
	if len(response.Choices) != 1 || (response.Choices[0].Finish != "stop" && response.Choices[0].Finish != "tool_calls") {
		return Message{}, ports.ErrUnavailable
	}
	return response.Choices[0].Message, nil
}

// Script is an explicit deterministic model for tests, never a live fallback.
type Script struct {
	Replies []Message
	Index   int
}

func (s *Script) Complete(ctx context.Context, _ []Message) (Message, error) {
	if e := ctx.Err(); e != nil {
		return Message{}, e
	}
	if s.Index >= len(s.Replies) {
		return Message{}, ports.ErrUnavailable
	}
	m := s.Replies[s.Index]
	s.Index++
	return m, nil
}
