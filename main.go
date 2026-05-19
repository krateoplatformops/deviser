package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/krateoplatformops/deviser/internal/config"
	"github.com/krateoplatformops/deviser/internal/pg"
	"github.com/krateoplatformops/deviser/internal/telemetry"
	"github.com/krateoplatformops/deviser/internal/util/daemon"
	"github.com/krateoplatformops/plumbing/pgutil"
	"github.com/krateoplatformops/plumbing/server/probes"
)

func main() {
	cfg := config.Setup()

	rootCtx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	metrics, shutdownMetrics, err := telemetry.Setup(rootCtx, cfg.Log, telemetry.Config{
		Enabled:        cfg.OTelEnabled,
		ServiceName:    "deviser",
		ExportInterval: cfg.OTelExportInterval,
	})
	if err != nil {
		cfg.Log.Error("OpenTelemetry setup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownMetrics(ctx); err != nil {
			cfg.Log.Warn("OpenTelemetry shutdown failed", slog.Any("err", err))
		}
	}()

	pgCtx, cancel := context.WithTimeout(rootCtx, cfg.DbReadyTimeout)
	defer cancel()

	dbConnectStarted := time.Now()
	pool, err := pgutil.WaitForPostgres(pgCtx, cfg.Log, cfg.DbURL)
	metrics.RecordDBConnectDuration(rootCtx, time.Since(dbConnectStarted))
	if err != nil {
		metrics.IncStartupFailure(rootCtx)
		cfg.Log.Error("cannot connect to PostgreSQL", slog.Any("err", err))
		os.Exit(1)
	}
	defer pool.Close()
	cfg.Log.Info("PostgreSQL is ready.")

	schemas := []string{"k8s_events.schema.sql", "resources.schema.sql"}
	for _, schema := range schemas {
		sql := cfg.MustLoadSQL(schema)
		schemaStarted := time.Now()
		_, err = pool.Exec(rootCtx, sql)
		metrics.RecordSchemaApplyDuration(rootCtx, time.Since(schemaStarted))
		if err != nil {
			metrics.IncSchemaApplyFailure(rootCtx)
			metrics.IncStartupFailure(rootCtx)
			cfg.Log.Error("SQL failed",
				slog.String("schema", schema),
				slog.String("sql", sql),
				slog.Any("err", err))
			os.Exit(1)
		}
	}

	migrationAssets := cfg.MustLoadMigrations()
	migrations := make([]pg.Migration, 0, len(migrationAssets))
	for _, migration := range migrationAssets {
		migrations = append(migrations, pg.Migration{
			Version: migration.Version,
			SQL:     migration.SQL,
		})
	}

	if err := pg.ApplyMigrations(rootCtx, pool, cfg.Log, migrations); err != nil {
		metrics.IncStartupFailure(rootCtx)
		cfg.Log.Error("database migrations failed", slog.Any("err", err))
		os.Exit(1)
	}

	// -----------------------------
	// Partition management loop
	// -----------------------------
	cfg.Log.Info("Starting daily partition manager loop...")

	hs := probes.New(cfg.Log, pool, cfg.Port)
	hs.Start()

	co := pg.CreateDailyPartitionsOptions{
		Pool:    pool,
		Log:     cfg.Log,
		Tpl:     cfg.MustLoadSQLTemplate("partition.tpl.sql", "partition"),
		Days:    cfg.DbPartitionDays,
		Metrics: metrics,
	}
	purgeOpts := pg.PurgeDeletedResourcesOptions{
		Pool:          pool,
		Log:           cfg.Log,
		RetentionDays: cfg.SoftDeleteRetentionDays,
		BatchSize:     cfg.SoftDeletePurgeBatchSize,
		Metrics:       metrics,
	}

	pm := &pg.PartitionManager{
		Pool:                   pool,
		Log:                    cfg.Log,
		Metrics:                metrics,
		ParentTable:            "k8s_events",
		RetentionDays:          cfg.PmRetentionDays,
		MaxPartitionsSizeBytes: cfg.PmMaxPartitionsSizeBytes,
		TriggerRatio:           cfg.PmTriggerRatio,
		TargetRatio:            cfg.PmTargetRatio,
		DryRun:                 cfg.PmDryRun,
	}

	metrics.IncStartupSuccess(rootCtx)

	err = daemon.Run(
		cfg.Log,
		// main service loop
		func(ctx context.Context) error {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()

			// First immediate run
			hadError := false
			if err := pg.CreateDailyPartitions(ctx, &co); err != nil {
				hadError = true
				cfg.Log.Error("failed to create daily partitions", slog.Any("err", err))
			}
			if _, err := pg.PurgeDeletedResources(ctx, &purgeOpts); err != nil {
				hadError = true
				cfg.Log.Error("failed to purge deleted resources", slog.Any("err", err))
			}
			if err := pm.Maintain(ctx); err != nil {
				hadError = true
				cfg.Log.Error("partition maintenance failed", slog.Any("err", err))
			}
			if hadError {
				metrics.IncLoopIterationFailure(ctx)
			} else {
				metrics.IncLoopIterationSuccess(ctx)
			}

			for {
				select {
				case <-ctx.Done():
					cfg.Log.Info("main loop stopped")
					return nil
				case <-ticker.C:
					hadError = false
					if err := pg.CreateDailyPartitions(ctx, &co); err != nil {
						hadError = true
						cfg.Log.Error("partition creation failed", slog.Any("err", err))
					}

					if _, err := pg.PurgeDeletedResources(ctx, &purgeOpts); err != nil {
						hadError = true
						cfg.Log.Error("failed to purge deleted resources", slog.Any("err", err))
					}

					if err := pm.Maintain(ctx); err != nil {
						hadError = true
						cfg.Log.Error("partition maintenance failed", slog.Any("err", err))
					}
					if hadError {
						metrics.IncLoopIterationFailure(ctx)
					} else {
						metrics.IncLoopIterationSuccess(ctx)
					}
				}
			}
		},

		// shutdown callbacks (HTTP server, DB...)
		func(ctx context.Context) error { return hs.Shutdown(ctx) },
		func(ctx context.Context) error { pool.Close(); return nil },
	)

	if err != nil {
		cfg.Log.Error("service error", slog.Any("err", err))
		os.Exit(1)
	}
}
