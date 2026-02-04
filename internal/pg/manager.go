package pg

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PartitionManager struct {
	Pool *pgxpool.Pool // Connection pool to Postgres database
	Log  *slog.Logger  // Logger for debug/info/warn/error messages

	ParentTable string // Name of the parent partitioned table to manage

	RetentionDays int // Number of days to keep partitions; older partitions will be dropped

	MaxPartitionsSizeBytes uint64  // Maximum total size of all partitions in bytes; triggers quota-based cleanup
	TriggerRatio           float64 // Fraction of MaxPartitionsSizeBytes to start cleanup (e.g., 0.75 = 75%)
	TargetRatio            float64 // Fraction of MaxPartitionsSizeBytes to reach after cleanup (e.g., 0.60 = 60%)

	DryRun bool // If true, no partitions are dropped; actions are logged only
}

func (pm *PartitionManager) Maintain(ctx context.Context) error {

	locked, err := pm.tryLock(ctx)
	if err != nil || !locked {
		return err
	}

	parts, err := pm.listPartitions(ctx)
	if err != nil {
		return fmt.Errorf("failed to list partitions: %w", err)
	}

	for _, p := range parts {
		pm.Log.Debug("partition discovered",
			slog.String("name", p.name),
			slog.Time("start", p.start),
			slog.Time("end", p.end),
			slog.Int64("size_bytes", p.size),
		)
	}

	if err := pm.dropExpired(ctx, parts); err != nil {
		return err
	}

	if pm.MaxPartitionsSizeBytes > 0 {
		var used uint64
		for _, p := range parts {
			used += uint64(p.size)
		}
		if err := pm.enforceQuota(ctx, parts, used); err != nil {
			return err
		}
	}

	return nil
}

func (pm *PartitionManager) tryLock(ctx context.Context) (bool, error) {
	var ok bool

	err := pm.Pool.QueryRow(ctx,
		`SELECT pg_try_advisory_lock($1)`,
		int64(88442211),
	).Scan(&ok)

	return ok, err
}

func (pm *PartitionManager) dropExpired(ctx context.Context, parts []partitionInfo) error {

	cutoff := time.Now().AddDate(0, 0, -pm.RetentionDays)

	for _, p := range parts {
		if p.start.Before(cutoff) {

			pm.Log.Info("dropping expired partition",
				slog.String("partition", p.name),
				slog.Bool("dry_run", pm.DryRun),
			)

			if !pm.DryRun {
				_, err := pm.Pool.Exec(ctx,
					"DROP TABLE IF EXISTS "+pgx.Identifier{p.name}.Sanitize(),
				)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (pm *PartitionManager) listPartitions(ctx context.Context) ([]partitionInfo, error) {

	const q = `
        SELECT
            c.relname,
            pg_get_expr(c.relpartbound, c.oid),
            pg_total_relation_size(c.oid)
        FROM pg_class c
        JOIN pg_inherits i ON i.inhrelid = c.oid
        WHERE i.inhparent = $1::regclass;
    `

	rows, err := pm.Pool.Query(ctx, q, pm.ParentTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var parts []partitionInfo

	for rows.Next() {
		var (
			name  string
			bound string
			size  int64
		)

		if err := rows.Scan(&name, &bound, &size); err != nil {
			return nil, err
		}

		start, end, err := parsePartitionBound(bound)
		if err != nil {
			pm.Log.Warn(
				"failed to parse partition bound",
				slog.String("partition", name),
				slog.String("bound", bound),
				slog.Any("err", err),
			)
			continue
		}

		parts = append(parts, partitionInfo{
			name:  name,
			start: start,
			end:   end,
			size:  size,
		})
	}

	return parts, rows.Err()
}

func parsePartitionBound(bound string) (time.Time, time.Time, error) {

	const layout = "2006-01-02"

	// Estraggo le date
	var from, to string
	_, err := fmt.Sscanf(
		bound,
		"FOR VALUES FROM ('%s') TO ('%s')",
		&from,
		&to,
	)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	from = strings.TrimSuffix(from, "')")
	to = strings.TrimSuffix(to, "')")

	start, err := time.Parse(layout, from)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	end, err := time.Parse(layout, to)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return start, end, nil
}

func (pm *PartitionManager) enforceQuota(ctx context.Context, parts []partitionInfo, used uint64) error {
	trigger := uint64(float64(pm.MaxPartitionsSizeBytes) * pm.TriggerRatio)
	if used < trigger {
		return nil
	}

	target := uint64(float64(pm.MaxPartitionsSizeBytes) * pm.TargetRatio)
	bytesToFree := used - target

	pm.Log.Warn("quota exceeded, starting cleanup",
		slog.Uint64("used", used),
		slog.Uint64("bytes_to_free", bytesToFree),
		slog.Bool("dry_run", pm.DryRun),
	)

	// Ordina per data inizio crescente
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].start.Before(parts[j].start)
	})

	var freed uint64
	for _, p := range parts {
		pm.Log.Warn("dropping partition for quota",
			slog.String("partition", p.name),
			slog.Int64("size", p.size),
			slog.Bool("dry_run", pm.DryRun),
		)

		if !pm.DryRun {
			_, err := pm.Pool.Exec(ctx, "DROP TABLE IF EXISTS "+pgx.Identifier{p.name}.Sanitize())
			if err != nil {
				return err
			}
		}

		freed += uint64(p.size)
		if freed >= bytesToFree {
			break
		}
	}

	return nil
}

type partitionInfo struct {
	name  string
	start time.Time
	end   time.Time
	size  int64
}
