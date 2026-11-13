package app

import (
	"database/sql"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/drafts"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
)

func demoDependencies(db *sql.DB, provider *integrations.Provider, ai *config.AIConfig) Dependencies {
	deps := Dependencies{Access: provider, ResolutionGenerator: knowledge.ResolutionGenerator{Demo: true}}
	if ai == nil {
		return deps
	}
	deps.ResolutionGenerator = knowledge.ResolutionGenerator{Client: providerhttp.Client{BaseURL: ai.ModelBaseURL, Key: ai.ModelKey}, Model: ai.ModelName}
	k := &knowledge.Service{DB: db, Embedder: &knowledge.Jina{
		Client: providerhttp.Client{BaseURL: ai.EmbeddingBaseURL, Key: ai.EmbeddingKey},
	}}
	requests := accessrequests.Store{DB: db}
	deps.Knowledge, deps.Requests = k, requests
	model := &agent.DeepSeek{
		Client: providerhttp.Client{BaseURL: ai.ModelBaseURL, Key: ai.ModelKey},
		Model:  ai.ModelName,
	}
	deps.Agent = &agent.Agent{Model: model, Classifier: model,
		Knowledge: k, Tasks: provider, Access: provider,
		Checkpoints: onboarding.SQLStore{DB: db},
		Drafts:      drafts.New(requests), Requests: requests,
	}
	return deps
}
