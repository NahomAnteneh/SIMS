package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/infrastructure/sims/internal/config"
	"github.com/infrastructure/sims/internal/logging"
	"github.com/infrastructure/sims/internal/server"
)

const version = "0.1.0"

func main() {
	cfg := config.LoadQueryAPIConfig()
	logger := logging.DefaultLogger(cfg.LogLevel)

	logger.Info("initializing vSphere query API service",
		slog.String("version", version),
		slog.String("port", cfg.Port),
		slog.String("database_url", cfg.DatabaseURL), // Will be redacted
	)

	srv := server.New("sims-query-api", version, cfg.Port, logger)

	srv.AddReadinessCheck("database_connection", func(_ context.Context) error {
		// In Sprint 0, readiness signals process initialized and ready
		return nil
	})

	if err := srv.Run(); err != nil {
		logger.Error("query API encountered fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
