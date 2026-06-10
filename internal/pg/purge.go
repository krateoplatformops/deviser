package pg

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krateoplatformops/deviser/internal/telemetry"
)

// defaultPurgeBatchSize is the fallback batch size used when a caller does not
// supply a positive BatchSize. In production main.go always passes the
// configured value (see config.defaultSoftDeletePurgeBatchSize); this guards
// direct and test callers.
const defaultPurgeBatchSize = 1000

type PurgeDeletedResourcesOptions struct {
	Pool          *pgxpool.Pool
	Log           *slog.Logger
	RetentionDays int
	BatchSize     int
	Metrics       *telemetry.Metrics
}

func PurgeDeletedResources(ctx context.Context, opts *PurgeDeletedResourcesOptions) (totalDeleted int64, retErr error) {
	started := time.Now()
	defer func() {
		opts.Metrics.RecordResourcesPurgeDuration(ctx, time.Since(started))
		opts.Metrics.AddResourcesPurgeRows(ctx, totalDeleted)
		if retErr != nil {
			opts.Metrics.IncResourcesPurgeFailure(ctx)
		}
	}()

	if opts.RetentionDays <= 0 {
		opts.Log.Debug("soft-delete purge skipped because retention is disabled", slog.Int("retention_days", opts.RetentionDays))
		return 0, nil
	}

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultPurgeBatchSize
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -opts.RetentionDays)

	batches := 0
	for {
		result, err := opts.Pool.Exec(ctx, `
            DELETE FROM krateo_resources
            WHERE id IN (
                SELECT id
                FROM krateo_resources
                WHERE deleted_at IS NOT NULL
                  AND deleted_at < $1
                ORDER BY id
                LIMIT $2
            )
        `,
			cutoff,
			batchSize,
		)
		if err != nil {
			opts.Log.Error("failed to purge soft-deleted resources",
				slog.Any("err", err),
				slog.Time("cutoff", cutoff),
				slog.Int64("rows_deleted", totalDeleted),
				slog.Int("batches", batches),
				slog.Int64("duration_ms", time.Since(started).Milliseconds()))
			return totalDeleted, err
		}

		batches++
		deleted := result.RowsAffected()
		totalDeleted += deleted

		if deleted < int64(batchSize) {
			break
		}
	}

	elapsed := time.Since(started)

	// Nothing was eligible for hard deletion this cycle. This is the steady
	// state (it runs hourly), so keep it at debug to avoid implying a purge
	// happened when no rows were removed.
	if totalDeleted == 0 {
		opts.Log.Debug("soft-delete purge ran, no resources past retention",
			slog.Int("retention_days", opts.RetentionDays),
			slog.Time("cutoff", cutoff),
			slog.Int64("duration_ms", elapsed.Milliseconds()))
		return 0, nil
	}

	opts.Log.Info("purged soft-deleted resources",
		slog.Int64("rows_deleted", totalDeleted),
		slog.Int("batches", batches),
		slog.Int("retention_days", opts.RetentionDays),
		slog.Int("batch_size", batchSize),
		slog.Time("cutoff", cutoff),
		slog.Int64("duration_ms", elapsed.Milliseconds()))

	return totalDeleted, nil
}
