package app

import (
	"database/sql"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/drafts"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
)

func demoDependencies(db *sql.DB, provider *integrations.Provider, ai *config.AIConfig) Dependencies {
	deps := Dependencies{Access: provider}
	if ai == nil {
		return deps
	}
	k := &knowledge.Service{DB: db, Embedder: &knowledge.Jina{
		Client: providerhttp.Client{BaseURL: ai.EmbeddingBaseURL, Key: ai.EmbeddingKey},
	}}
	requests := accessrequests.Store{DB: db}
	deps.Knowledge, deps.Requests = k, requests
	deps.Agent = &agent.Agent{
		Model: &agent.DeepSeek{
			Client: providerhttp.Client{BaseURL: ai.ModelBaseURL, Key: ai.ModelKey},
			Model:  ai.ModelName,
		},
		Knowledge: k, Tasks: provider, Access: provider,
		Drafts: drafts.New(requests), Requests: requests,
	}
	return deps
}
