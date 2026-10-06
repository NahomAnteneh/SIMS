package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
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
		slog.String("database_url", cfg.DatabaseURL), // Redacted by slog handler
	)

	// Configure pgxpool for read-only queries
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to parse database configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// S1 sizing guidelines from docs/database.md:
	// query_ro pool: max 20 connections, min 2 connections, max conn lifetime 1 hour
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		logger.Error("failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	srv := server.New("sims-query-api", version, cfg.Port, logger)

	// Add database readiness ping
	srv.AddReadinessCheck("database_connection", func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("database unreachable: %w", err)
		}
		return nil
	})

	// Configure CORS middleware if specified (restrictive by default)
	if cfg.CORSAllowedOrigins != "" {
		corsMw := api.NewCORSMiddleware(cfg.CORSAllowedOrigins)
		srv.Use(corsMw.Handler)
		logger.Info("configured CORS middleware", slog.String("allowed_origins", cfg.CORSAllowedOrigins))
	}

	// Configure OIDC / JWT Authentication middleware
	authCfg := auth.Config{
		IssuerURL:    os.Getenv("OIDC_ISSUER_URL"),
		Audience:     os.Getenv("OIDC_AUDIENCE"),
		JWKSURL:      os.Getenv("OIDC_JWKS_URL"),
		AuthDisabled: os.Getenv("AUTH_ENABLED") == "false" || os.Getenv("OIDC_ISSUER_URL") == "",
		Env:          os.Getenv("ENV"),
	}
	authenticator, err := auth.NewAuthenticator(ctx, authCfg)
	if err == nil {
		srv.Use(authenticator.Middleware)
		logger.Info("configured authentication middleware", slog.Bool("auth_disabled", authCfg.AuthDisabled))
	} else {
		logger.Warn("failed to initialize authenticator", slog.String("error", err.Error()))
	}

	// Register API endpoints
	apiHandler := api.NewHandler(pool, logger)
	apiHandler.RegisterRoutes(srv.Mux)

	if err := srv.Run(); err != nil {
		logger.Error("query API encountered fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
