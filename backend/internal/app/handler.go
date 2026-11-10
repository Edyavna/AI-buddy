package app

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// NewHandler is the composition root. Supplying AccessChecker connects #13
// without changing HTTP handlers. Explicit demo mode installs the persistent simulator.
func NewHandler(cfg config.Config, db *sql.DB, logger *slog.Logger, ready func(context.Context) error, access ports.AccessChecker) http.Handler {
	opts := httpapi.Options{Logger: logger, AllowedOrigins: cfg.AllowedOrigins, Readiness: ready}
	if db != nil {
		if access == nil && cfg.DemoMode {
			access = integrations.New(db)
		}
		session := auth.Handler{Service: &auth.Service{Store: auth.SQLStore{DB: db}, TTL: cfg.SessionTTL}, DemoMode: cfg.DemoMode, CookieSecure: cfg.SessionCookieSecure}
		plans := onboarding.Handler{Service: onboarding.Service{Store: onboarding.SQLStore{DB: db}, Access: access}}
		opts.Register = func(mux *http.ServeMux) { session.Register(mux); plans.Register(mux, session) }
	}
	return httpapi.New(opts)
}
