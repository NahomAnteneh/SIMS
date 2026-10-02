package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/infrastruture/sims/internal/config"
	"github.com/infrastruture/sims/internal/logging"
	"github.com/infrastruture/sims/internal/server"
)

const version = "0.1.0"

func main() {
	cfg := config.LoadCollectorConfig()
	logger := logging.DefaultLogger(cfg.LogLevel)

	logger.Info("initializing vSphere collector service",
		slog.String("version", version),
		slog.String("port", cfg.Port),
		slog.String("vcenter_url", cfg.VCenterURL),
		slog.String("vcenter_username", cfg.VCenterUsername),
		slog.String("vcenter_password", cfg.VCenterPassword), // Will be redacted
		slog.Bool("insecure_tls", cfg.InsecureTLS),
	)

	srv := server.New("sims-collector", version, cfg.Port, logger)

	srv.AddReadinessCheck("collector_runtime", func(_ context.Context) error {
		// In Sprint 0, readiness signals process initialized and ready for future worker leases
		return nil
	})

	if err := srv.Run(); err != nil {
		logger.Error("collector encountered fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
