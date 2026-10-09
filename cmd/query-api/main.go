package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/config"
	"github.com/infrastructure/sims/internal/database"
	"github.com/infrastructure/sims/internal/logging"
	"github.com/infrastructure/sims/internal/server"
)

const version = "1.0.0"

func generateSecurePassword(length int) string {
	const charset = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%^&*"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "SimsAdmin" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

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

	if cfg.DatabaseURL == "" {
		err := fmt.Errorf("DATABASE_URL environment variable is required")
		logger.Error("missing database configuration", slog.String("error", err.Error()))
		return err
	}

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

	// Bootstrap administrator user if users table is completely empty
	existingUsers, listErr := queries.ListUsers(initCtx)
	if listErr == nil && len(existingUsers) == 0 {
		adminUsername := os.Getenv("ADMIN_USERNAME")
		if adminUsername == "" {
			adminUsername = os.Getenv("ADMIN_INITIAL_USERNAME")
		}
		if adminUsername == "" {
			adminUsername = os.Getenv("SIMS_ADMIN_USERNAME")
		}
		if adminUsername == "" {
			adminUsername = "admin"
		}

		adminPassword := os.Getenv("ADMIN_PASSWORD")
		if adminPassword == "" {
			adminPassword = os.Getenv("ADMIN_INITIAL_PASSWORD")
		}
		if adminPassword == "" {
			adminPassword = os.Getenv("SIMS_ADMIN_PASSWORD")
		}
		generated := false
		if adminPassword == "" {
			adminPassword = generateSecurePassword(16)
			generated = true
		}
		pwdHash, hashErr := auth.HashPassword(adminPassword)
		if hashErr != nil {
			logger.Error("failed to hash initial admin password", slog.String("error", hashErr.Error()))
		} else {
			_, createErr := queries.CreateUser(initCtx, db.CreateUserParams{
				Username:     adminUsername,
				PasswordHash: pwdHash,
				Email:        pgtype.Text{String: adminUsername + "@local", Valid: true},
				Role:         string(auth.RoleAdmin),
			})
			if createErr != nil {
				logger.Warn("failed to bootstrap admin user", slog.String("error", createErr.Error()))
			} else if generated {
				logger.Warn("================================================================================")
				logger.Warn("BOOTSTRAP: Initial administrator account provisioned.")
				logger.Warn("Username: " + adminUsername)
				logger.Warn("Password: " + adminPassword)
				logger.Warn("IMPORTANT: Save this password and change it immediately after initial login.")
				logger.Warn("================================================================================")
			} else {
				logger.Info("bootstrapped initial admin user from environment configuration", slog.String("username", adminUsername))
			}
		}
	}

	tokenSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(tokenSecret) == 0 {
		if os.Getenv("ENV") == "test" || os.Getenv("ENV") == "testing" {
			tokenSecret = auth.DefaultJWTSecret
		} else {
			randomSecret := make([]byte, 32)
			if _, err := rand.Read(randomSecret); err != nil {
				return fmt.Errorf("failed to generate random JWT secret: %w", err)
			}
			tokenSecret = randomSecret
			logger.Warn("JWT_SECRET not configured; generated ephemeral random 32-byte signing secret for this session")
		}
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
