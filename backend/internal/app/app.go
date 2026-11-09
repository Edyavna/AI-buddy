package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
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
	server := &http.Server{
		Handler:           httpapi.New(httpapi.Options{Logger: logger, AllowedOrigins: cfg.AllowedOrigins, Readiness: ready}),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
		// WriteTimeout=0 for future SSE. Chat owns its 120s generation deadline.
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
