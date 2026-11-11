package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	var db *sql.DB
	if cfg.DatabaseEnabled {
		startup, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
		defer cancel()
		var err error
		db, err = database.Open(startup, cfg.DatabaseURL)
		if err != nil {
			return errors.New("database connection failed; check DATABASE_URL and database availability")
		}
		defer db.Close()
		if err := migrations.Verify(startup, db, migrations.Files); err != nil {
			return errors.New("database schema check failed; run cmd/migrate with the same DATABASE_URL")
		}
		// Recovery may interrupt all unfinished messages. Refuse a second active
		// process for this schema rather than interrupting another live API instance.
		guard, err := db.Conn(startup)
		if err != nil {
			return errors.New("chat runtime lock unavailable")
		}
		defer guard.Close()
		var locked bool
		if err := guard.QueryRowContext(startup, `SELECT pg_try_advisory_lock(1729010,hashtext(current_schema()))`).Scan(&locked); err != nil || !locked {
			return errors.New("chat runtime requires one active API process per database schema")
		}
		if err := (chat.SQLStore{DB: db}).Recover(startup); err != nil {
			return errors.New("chat recovery failed; check database availability")
		}
	} else {
		logger.Warn("database disabled; infrastructure-only mode, business API is unavailable")
	}
	ready := func(ctx context.Context) error {
		if db == nil {
			return errors.New("database disabled")
		}
		return db.PingContext(ctx)
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return errors.New("HTTP listener failed; check HTTP_ADDR and port availability")
	}
	defer listener.Close()
	logger.Info("server listening", "address", listener.Addr().String(), "database_enabled", cfg.DatabaseEnabled)
	var deps Dependencies
	if db != nil && cfg.DemoMode {
		provider := integrations.New(db)
		deps.Access = provider
		worker := accessrequests.Worker{Store: accessrequests.Store{DB: db}, Provider: provider}
		workerCtx, stopWorker := context.WithCancel(ctx)
		workerDone := make(chan struct{})
		go func() {
			defer close(workerDone)
			worker.Run(workerCtx, logger)
		}()
		defer func() {
			stopWorker()
			<-workerDone
		}()
	}
	server := &http.Server{
		Handler:           NewHandler(cfg, db, logger, ready, deps),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
		// WriteTimeout=0 for SSE. Chat owns its 120s generation deadline.
		BaseContext: func(net.Listener) context.Context { return ctx },
		ErrorLog:    slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return Serve(ctx, server, listener, cfg.ShutdownTimeout)
}

// Serve waits for in-flight requests, then closes connections on shutdown timeout.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		<-done
		return err
	}
}
