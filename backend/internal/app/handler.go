package app

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/curator"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledgegaps"
	"github.com/Edyavna/AI-buddy/backend/internal/meetings"
	"github.com/Edyavna/AI-buddy/backend/internal/mentor"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/reviewprep"
)

type Dependencies struct {
	Access              ports.AccessChecker
	Agent               ports.Agent
	Requests            ports.RequestReader
	Knowledge           ports.Knowledge
	Questions           ports.OpenQuestionCounter
	QuestionDocuments   knowledgegaps.Documents
	ResolutionGenerator knowledge.ResolutionGenerator
}

// NewHandler is the composition root. Supplying AccessChecker connects #13
// without changing HTTP handlers. Explicit demo mode installs the persistent simulator.
func NewHandler(cfg config.Config, db *sql.DB, logger *slog.Logger, ready func(context.Context) error, deps Dependencies) http.Handler {
	opts := httpapi.Options{Logger: logger, AllowedOrigins: cfg.AllowedOrigins, Readiness: ready}
	if db != nil {
		if deps.Access == nil && cfg.DemoMode {
			deps.Access = integrations.New(db)
		}
		if deps.Agent == nil && cfg.DemoMode {
			deps.Agent = demo.Agent{Delay: cfg.DemoChatDelay}
		}
		requestStore := accessrequests.Store{DB: db}
		requests := accessrequests.Handler{Store: requestStore, Checker: deps.Access}
		if deps.Requests == nil {
			deps.Requests = requestStore
		}
		if deps.Knowledge == nil {
			deps.Knowledge = &knowledge.Service{DB: db}
		}
		session := auth.Handler{Service: &auth.Service{Store: auth.SQLStore{DB: db}, TTL: cfg.SessionTTL}, DemoMode: cfg.DemoMode, CookieSecure: cfg.SessionCookieSecure}
		plans := onboarding.Handler{Service: onboarding.Service{Store: onboarding.SQLStore{DB: db}, Access: deps.Access}}
		mentors := mentor.Store{DB: db}
		checkpoints := onboarding.CheckpointHandler{Service: onboarding.Checkpoints{DB: db, Access: deps.Access, Assignments: mentors}}
		conversations := chat.Handler{Service: chat.Service{Store: chat.SQLStore{DB: db}, Agent: deps.Agent, Requests: deps.Requests, Knowledge: deps.Knowledge}, Logger: logger}
		uploads := knowledge.Uploads{DB: db}
		if deps.QuestionDocuments == nil {
			deps.QuestionDocuments = uploads
		}
		questions := knowledgegaps.Store{DB: db, Documents: deps.QuestionDocuments, Knowledge: deps.Knowledge}
		if deps.Questions == nil {
			deps.Questions = questions
		}
		reviews := reviewprep.Store{DB: db}
		if cfg.GitHubToken != "" || cfg.GitHubAPIURL != "" {
			reviews.Provider = reviewprep.GitHubClient{Token: cfg.GitHubToken, BaseURL: cfg.GitHubAPIURL}
		}
		opts.Register = func(mux *http.ServeMux) {
			session.Register(mux)
			hrscope.Store{DB: db}.Register(mux, session)
			plans.Register(mux, session)
			mentor.Handler{Store: mentors, Questions: deps.Questions, Access: deps.Access}.Register(mux, session)
			checkpoints.Register(mux, session)
			knowledgegaps.Handler{Store: questions}.Register(mux, session)
			reviewprep.Handler{Store: reviews}.Register(mux, session)
			meetings.Store{DB: db}.Register(mux, session)
			curator.Store{DB: db}.Register(mux, session)
			uploads.Register(mux, session)
			uploads.RegisterResolutions(mux, session, deps.ResolutionGenerator)
			uploads.RegisterGroups(mux, session)
			requests.Register(mux, session)
			mux.Handle("GET /api/v1/documents/{document_id}", session.Require(knowledge.Handler(deps.Knowledge), false))
			conversations.Register(mux, session)
		}
	}
	return httpapi.New(opts)
}
