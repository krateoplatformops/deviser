package pg

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Migration struct {
	Version string
	SQL     string
}

type migrationTx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, migrations []Migration) error {
	if _, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := appliedMigrations(ctx, pool)
	if err != nil {
		return err
	}

	for _, migration := range PendingMigrations(migrations, applied) {
		if err := applyMigration(ctx, pool, migration); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Version, err)
		}

		log.Info("database migration applied", slog.String("version", migration.Version))
	}

	return nil
}

func PendingMigrations(migrations []Migration, applied map[string]struct{}) []Migration {
	ordered := append([]Migration(nil), migrations...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Version < ordered[j].Version
	})

	pending := make([]Migration, 0, len(ordered))
	for _, migration := range ordered {
		if _, ok := applied[migration.Version]; ok {
			continue
		}
		pending = append(pending, migration)
	}

	return pending
}

func appliedMigrations(ctx context.Context, pool *pgxpool.Pool) (map[string]struct{}, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]struct{}{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}

	return applied, nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, migration Migration) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	err = applyMigrationTx(ctx, tx, migration)
	return err
}

func applyMigrationTx(ctx context.Context, tx migrationTx, migration Migration) error {
	if _, err := tx.Exec(ctx, migration.SQL); err != nil {
		return fmt.Errorf("execute SQL: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`,
		migration.Version); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
