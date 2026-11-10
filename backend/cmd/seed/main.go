package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("demo seed failed", "error", err.Error())
		os.Exit(1)
	}
	slog.Info("demo seed applied; existing progress preserved")
}
func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if !cfg.DemoMode || !cfg.DatabaseEnabled {
		return errors.New("seed requires DEMO_MODE=true and DATABASE_ENABLED=true")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("database connection failed; check configuration and availability")
	}
	defer db.Close()
	if err := migrations.Verify(ctx, db, migrations.Files); err != nil {
		return errors.New("schema check failed; run migrate first")
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		return errors.New("demo seed failed; check schema and database availability")
	}
	return nil
}
