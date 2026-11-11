package config

import (
	"maps"
	"strings"
	"testing"
)

func TestLiveAIConfig(t *testing.T) {
	valid := map[string]string{
		"AI_MODE": "live", "DEMO_MODE": "true",
		"DATABASE_URL": "postgres://user:password@localhost/db",
		"LLM_BASE_URL": "http://127.0.0.1:1234",
		"LLM_MODEL":    "selected-model", "LLM_API_KEY": "private-model-key",
		"EMBEDDING_BASE_URL": "https://example.invalid/v1", "EMBEDDING_API_KEY": "private-embedding-key",
	}
	for _, tt := range []struct{ key, value, want string }{
		{"AI_MODE", "demo", ""},
		{"AI_MODE", "live", ""},
		{"AI_MODE", "unknown", "AI_MODE"},
		{"DEMO_MODE", "false", "DEMO_MODE"},
		{"DATABASE_ENABLED", "false", "DATABASE_ENABLED"},
		{"LLM_MODEL", "", "LLM_MODEL"},
		{"LLM_API_KEY", " ", "LLM_API_KEY"},
		{"EMBEDDING_BASE_URL", "http://example.invalid/v1", "EMBEDDING_BASE_URL"},
		{"EMBEDDING_BASE_URL", "https://user:private-url-key@example.invalid/v1", "EMBEDDING_BASE_URL"},
		{"EMBEDDING_API_KEY", "", "EMBEDDING_API_KEY"},
	} {
		t.Run(tt.key+" "+tt.value, func(t *testing.T) {
			env := maps.Clone(valid)
			env[tt.key] = tt.value
			cfg, err := Load(func(key string) string { return env[key] })
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "private-") {
					t.Fatalf("unexpected config error: %v", err)
				}
				return
			}
			if err != nil || (cfg.AI != nil) != (tt.value == "live") {
				t.Fatalf("unexpected AI mode: %v", err)
			}
		})
	}
}
