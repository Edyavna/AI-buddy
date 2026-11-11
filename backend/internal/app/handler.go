package app

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type Dependencies struct {
	Access   ports.AccessChecker
	Agent    ports.Agent
	Requests ports.RequestReader
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
		checker, _ := deps.Access.(ports.AccessProvider)
		requests := accessrequests.Handler{Store: requestStore, Checker: checker}
		if deps.Requests == nil {
			deps.Requests = requestStore
		}
		session := auth.Handler{Service: &auth.Service{Store: auth.SQLStore{DB: db}, TTL: cfg.SessionTTL}, DemoMode: cfg.DemoMode, CookieSecure: cfg.SessionCookieSecure}
		plans := onboarding.Handler{Service: onboarding.Service{Store: onboarding.SQLStore{DB: db}, Access: deps.Access}}
		conversations := chat.Handler{Service: chat.Service{Store: chat.SQLStore{DB: db}, Agent: deps.Agent, Requests: deps.Requests}, Logger: logger}
		opts.Register = func(mux *http.ServeMux) {
			session.Register(mux)
			plans.Register(mux, session)
			requests.Register(mux, session)
			conversations.Register(mux, session)
		}
	}
	return httpapi.New(opts)
}
