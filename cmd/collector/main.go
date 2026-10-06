package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/config"
	"github.com/infrastructure/sims/internal/logging"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/server"
	"github.com/infrastructure/sims/internal/vsphere"
)

const version = "0.2.0"

func main() {
	cfg := config.LoadCollectorConfig()
	logger := logging.DefaultLogger(cfg.LogLevel)

	logger.Info("initializing vSphere collector service",
		slog.String("version", version),
		slog.String("port", cfg.Port),
		slog.String("vcenter_url", cfg.VCenterURL),
		slog.String("vcenter_username", cfg.VCenterUsername),
		slog.String("vcenter_password", cfg.VCenterPassword), // Redacted by slog handler
		slog.Bool("insecure_tls", cfg.InsecureTLS),
	)

	// Configure pgxpool for bulk write operations
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to parse database configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	poolCfg.MaxConns = 10
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		logger.Error("failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	srv := server.New("sims-collector", version, cfg.Port, logger)

	// Readiness check: database connectivity
	srv.AddReadinessCheck("database_connection", func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("database unreachable: %w", err)
		}
		return nil
	})

	queries := db.New(pool)
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, srv.Metrics)

	// Lease Coordinator (S4.2)
	leaseCoord := collector.NewLeaseCoordinator(pool, queries, collector.LeaseConfig{
		LeaseTTL:          30 * time.Second,
		HeartbeatInterval: 10 * time.Second,
	}, logger)

	// Target Assignment FSM (S4.3)
	targetFSM := collector.NewTargetFSM(queries, logger)

	// PostgreSQL LISTEN/NOTIFY Listener (S4.4)
	listener := collector.NewPostgresListener(cfg.DatabaseURL, 60*time.Second, logger)

	// Concurrency Limiter & Rate Budgets (S4.5)
	limiter := collector.NewConcurrencyRegistry(2, 10, 4)

	// Fleet Manager (S4.5)
	fleet := collector.NewFleetManager(
		pool,
		queries,
		leaseCoord,
		targetFSM,
		listener,
		limiter,
		persister,
		collector.FleetConfig{
			WorkerID:               leaseCoord.WorkerID(),
			DefaultVCenterUsername: cfg.VCenterUsername,
			DefaultVCenterPassword: cfg.VCenterPassword,
			DefaultInsecureTLS:     cfg.InsecureTLS,
		},
		srv.Metrics,
		logger,
	)

	srv.AddReadinessCheck("fleet_coordinator", func(_ context.Context) error {
		return nil
	})

	// Bootstrap single-vCenter configuration if provided in environment
	if cfg.VCenterURL != "" {
		go func() {
			sessCfg := vsphere.SessionConfig{
				URL:            cfg.VCenterURL,
				Username:       cfg.VCenterUsername,
				Password:       cfg.VCenterPassword,
				Insecure:       cfg.InsecureTLS,
				RequestTimeout: 30 * time.Second,
			}
			session, err := vsphere.NewSessionManager(sessCfg)
			if err != nil {
				logger.Warn("bootstrap session config failed", slog.String("error", err.Error()))
				return
			}
			if err := session.Connect(ctx); err != nil {
				logger.Warn("bootstrap vCenter connect failed", slog.String("error", err.Error()))
				return
			}
			if err := session.Login(ctx); err != nil {
				logger.Warn("bootstrap vCenter login failed", slog.String("error", err.Error()))
				return
			}
			defer func() {
				logoutCtx, logoutCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer logoutCancel()
				_ = session.Logout(logoutCtx)
			}()

			about := session.Client().ServiceContent.About
			var vcenterID string
			err = pool.QueryRow(ctx, `
				INSERT INTO vcenters (name, api_url, instance_uuid, product_version, api_version, build)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (api_url) DO UPDATE
				SET name = EXCLUDED.name, instance_uuid = EXCLUDED.instance_uuid, updated_at = NOW()
				RETURNING id::text
			`, about.FullName, cfg.VCenterURL, about.InstanceUuid, about.Version, about.ApiVersion, about.Build).Scan(&vcenterID)
			if err != nil {
				logger.Warn("failed to upsert bootstrap vcenter record", slog.String("error", err.Error()))
				return
			}

			_, err = pool.Exec(ctx, `
				INSERT INTO collector_targets (vcenter_id, enabled)
				VALUES ($1::uuid, true)
				ON CONFLICT (vcenter_id) DO NOTHING
			`, vcenterID)
			if err != nil {
				logger.Warn("failed to insert bootstrap collector target", slog.String("error", err.Error()))
				return
			}
			logger.Info("bootstrap vCenter target registered in target registry", slog.String("vcenter_id", vcenterID))
		}()
	}

	// Start Fleet Manager
	if err := fleet.Start(ctx); err != nil {
		logger.Error("failed to start fleet manager", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = fleet.Stop(stopCtx)
	}()

	if err := srv.Run(); err != nil {
		logger.Error("collector encountered fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
