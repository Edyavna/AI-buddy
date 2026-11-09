package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(); err != nil {
		logger.Error("migration failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("migrations applied")
}
func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if !cfg.DatabaseEnabled {
		return errors.New("migrate requires DATABASE_ENABLED=true")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("database connection failed; check configuration and availability")
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db, migrations.Files); err != nil {
		return errors.New("SQL migration failed; check immutable migration names, checksums and SQL")
	}
	return nil
}
