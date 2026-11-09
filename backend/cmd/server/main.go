package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Edyavna/AI-buddy/backend/internal/app"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err.Error())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("server stopped")
}
