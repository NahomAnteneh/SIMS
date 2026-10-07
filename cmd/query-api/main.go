package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/config"
	"github.com/infrastructure/sims/internal/database"
	"github.com/infrastructure/sims/internal/logging"
	"github.com/infrastructure/sims/internal/server"
)

const version = "1.0.0"

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	versionFlag := flag.Bool("version", false, "print service version and exit")
	flag.BoolVar(versionFlag, "v", false, "print service version and exit (shorthand)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("sims-query-api v%s\n", version)
		return nil
	}

	cfg := config.LoadQueryAPIConfig()
	logger := logging.DefaultLogger(cfg.LogLevel)

	logger.Info("initializing vSphere query API service",
		slog.String("version", version),
		slog.String("port", cfg.Port),
		slog.String("database_url", logging.RedactURL(cfg.DatabaseURL)),
	)

	// Configure pgxpool for read-only queries with startup initialization context
	initCtx, initCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer initCancel()

	pool, err := database.NewBudgetedPool(initCtx, cfg.DatabaseURL, database.PoolQueryRO)
	if err != nil {
		logger.Error("failed to connect to database", slog.String("error", err.Error()))
		return err
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

	// Local Authentication & JWT Token Signer
	queries := db.New(pool)
	userStore := auth.NewDBUserStore(queries)
	tokenSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(tokenSecret) == 0 {
		tokenSecret = auth.DefaultJWTSecret
	}
	tokenSigner := auth.NewTokenSigner(tokenSecret, 24*time.Hour)

	// Configure Authentication middleware (supports local DB username/password, local Bearer JWTs, and OIDC)
	authCfg := auth.Config{
		IssuerURL:    os.Getenv("OIDC_ISSUER_URL"),
		Audience:     os.Getenv("OIDC_AUDIENCE"),
		JWKSURL:      os.Getenv("OIDC_JWKS_URL"),
		AuthDisabled: os.Getenv("AUTH_ENABLED") == "false",
		Env:          os.Getenv("ENV"),
	}
	authenticator, err := auth.NewAuthenticator(initCtx, authCfg)
	if err != nil {
		logger.Error("failed to initialize authenticator", slog.String("error", err.Error()))
		return err
	}
	authenticator.SetTokenSigner(tokenSigner)
	authenticator.SetUserStore(userStore)
	srv.Use(authenticator.Middleware)
	logger.Info("configured authentication middleware", slog.Bool("auth_disabled", authCfg.AuthDisabled))

	// Register API endpoints
	apiHandler := api.NewHandler(pool, logger)
	apiHandler.SetAuth(tokenSigner, userStore)
	apiHandler.RegisterRoutes(srv.Mux)

	if err := srv.Run(); err != nil {
		logger.Error("query API encountered fatal error", slog.String("error", err.Error()))
		return err
	}
	return nil
}
