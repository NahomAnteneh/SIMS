package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/config"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/logging"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/server"
	"github.com/infrastructure/sims/internal/vsphere"
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

	// S1 sizing guidelines from docs/database.md:
	// collector_rw pool: max 10 connections, min 2 connections, max conn lifetime 1 hour
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

	var initialSyncDone atomic.Bool
	srv.AddReadinessCheck("inventory_synced", func(_ context.Context) error {
		if !initialSyncDone.Load() {
			return fmt.Errorf("initial inventory sync in progress")
		}
		return nil
	})

	// Background worker loop
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
			logger.Error("failed to configure vCenter session manager", slog.String("error", err.Error()))
			return
		}

		logger.Info("connecting to vCenter session manager")
		if err := session.Connect(ctx); err != nil {
			logger.Error("failed to connect to vCenter", slog.String("error", err.Error()))
			return
		}
		if err := session.Login(ctx); err != nil {
			logger.Error("failed to login to vCenter", slog.String("error", err.Error()))
			return
		}
		defer func() {
			logoutCtx, logoutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer logoutCancel()
			_ = session.Logout(logoutCtx)
		}()

		client := session.Client()
		about := client.ServiceContent.About

		// Upsert vCenter record
		var vcenterID string
		err = pool.QueryRow(ctx, `
			INSERT INTO vcenters (name, api_url, instance_uuid)
			VALUES ($1, $2, $3)
			ON CONFLICT (api_url) DO UPDATE
			SET name = EXCLUDED.name, instance_uuid = EXCLUDED.instance_uuid, updated_at = NOW()
			RETURNING id::text
		`, about.FullName, cfg.VCenterURL, about.InstanceUuid).Scan(&vcenterID)
		if err != nil {
			logger.Error("failed to upsert vcenter entity", slog.String("error", err.Error()))
			return
		}

		logger.Info("vCenter entity established",
			slog.String("vcenter_id", vcenterID),
			slog.String("instance_uuid", about.InstanceUuid),
			slog.String("name", about.FullName),
		)

		hydrator := collector.NewHydrator(session, collector.HydratorConfig{MaxObjects: 500})
		persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, srv.Metrics)

		runSync := func() {
			syncStart := time.Now()
			logger.Info("starting topology hydration sync", slog.String("vcenter_id", vcenterID))

			snap, err := hydrator.HydrateTopology(ctx, vcenterID)
			if err != nil {
				logger.Error("topology hydration failed", slog.String("error", err.Error()))
				return
			}

			if err := persister.PersistTopology(ctx, snap); err != nil {
				logger.Error("topology persistence failed", slog.String("error", err.Error()))
				return
			}
			logger.Info("topology persisted successfully")

			totalVMs := 0
			err = hydrator.HydrateInventory(ctx, vcenterID, snap, func(batch []domain.VMRecord) error {
				persisted, err := persister.PersistVMBatch(ctx, batch)
				if err != nil {
					return err
				}
				totalVMs += persisted
				return nil
			})
			if err != nil {
				logger.Error("inventory hydration failed", slog.String("error", err.Error()))
				return
			}

			initialSyncDone.Store(true)
			logger.Info("inventory hydration and persistence complete",
				slog.Int("total_vms", totalVMs),
				slog.Duration("duration", time.Since(syncStart)),
			)
		}

		// Initial sync
		runSync()

		// Periodic sync if configured
		if cfg.SyncInterval > 0 {
			ticker := time.NewTicker(cfg.SyncInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runSync()
				}
			}
		}
	}()

	if err := srv.Run(); err != nil {
		logger.Error("collector encountered fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
