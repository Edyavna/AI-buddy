package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type AIConfig struct {
	ModelBaseURL, ModelName, ModelKey string
	EmbeddingBaseURL, EmbeddingKey    string
}

func loadAI(getenv func(string) string, cfg Config) (*AIConfig, error) {
	switch getenv("AI_MODE") {
	case "", "demo":
		return nil, nil
	case "live":
		if !cfg.DemoMode || !cfg.DatabaseEnabled {
			return nil, errors.New("AI_MODE=live requires DEMO_MODE=true and DATABASE_ENABLED=true")
		}
	default:
		return nil, errors.New("AI_MODE must be demo or live")
	}

	ai := &AIConfig{}
	for _, setting := range []struct {
		name   string
		target *string
	}{
		{"LLM_BASE_URL", &ai.ModelBaseURL},
		{"LLM_MODEL", &ai.ModelName},
		{"LLM_API_KEY", &ai.ModelKey},
		{"EMBEDDING_BASE_URL", &ai.EmbeddingBaseURL},
		{"EMBEDDING_API_KEY", &ai.EmbeddingKey},
	} {
		*setting.target = strings.TrimSpace(getenv(setting.name))
		if *setting.target == "" {
			return nil, fmt.Errorf("%s is required for AI_MODE=live", setting.name)
		}
	}
	for _, setting := range []struct{ name, value string }{
		{"LLM_BASE_URL", ai.ModelBaseURL}, {"EMBEDDING_BASE_URL", ai.EmbeddingBaseURL},
	} {
		u, err := url.Parse(setting.value)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return nil, fmt.Errorf("%s must be an HTTP base URL without credentials, query or fragment", setting.name)
		}
		localHTTP := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
		if u.Scheme != "https" && !localHTTP {
			return nil, fmt.Errorf("%s requires HTTPS outside localhost", setting.name)
		}
	}
	return ai, nil
}
