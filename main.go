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
	"github.com/krateoplatformops/deviser/internal/probes"
	"github.com/krateoplatformops/deviser/internal/util/daemon"
	pgutil "github.com/krateoplatformops/deviser/internal/util/pg"
)

func main() {
	cfg := config.Setup()

	rootCtx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	pgCtx, cancel := context.WithTimeout(rootCtx, cfg.DbReadyTimeout)
	defer cancel()

	pool, err := pgutil.WaitForPostgres(pgCtx, cfg.Log, cfg.DbURL)
	if err != nil {
		cfg.Log.Error("cannot connect to PostgreSQL", slog.Any("err", err))
		os.Exit(1)
	}
	defer pool.Close()
	cfg.Log.Info("PostgreSQL is ready.")

	sql := cfg.MustLoadSQL("schema.sql")
	_, err = pool.Exec(rootCtx, sql)
	if err != nil {
		cfg.Log.Error("SQL failed",
			slog.String("sql", sql),
			slog.Any("err", err))
		os.Exit(1)
	}

	// -----------------------------
	// Partition management loop
	// -----------------------------
	cfg.Log.Info("Starting daily partition manager loop...")

	hs := probes.New(cfg.Log, pool, cfg.Port)
	hs.Start()

	co := pg.CreateDailyPartitionsOptions{
		Pool: pool,
		Log:  cfg.Log,
		Tpl:  cfg.MustLoadSQLTemplate("partition.tpl.sql", "partition"),
		Days: cfg.DbPartitionDays,
	}

	err = daemon.Run(
		cfg.Log,
		// main service loop
		func(ctx context.Context) error {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()

			// First immediate run
			if err := pg.CreateDailyPartitions(ctx, &co); err != nil {
				cfg.Log.Error("failed to create daily partitions", slog.Any("err", err))
			}

			for {
				select {
				case <-ctx.Done():
					cfg.Log.Info("main loop stopped")
					return nil
				case <-ticker.C:
					if err := pg.CreateDailyPartitions(ctx, &co); err != nil {
						cfg.Log.Error("partition creation failed", slog.Any("err", err))
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
