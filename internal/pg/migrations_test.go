package pg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPendingMigrationsSortsAndSkipsApplied(t *testing.T) {
	migrations := []Migration{
		{Version: "010_second", SQL: "SELECT 2;"},
		{Version: "001_first", SQL: "SELECT 1;"},
		{Version: "020_third", SQL: "SELECT 3;"},
	}
	applied := map[string]struct{}{
		"010_second": {},
	}

	got := PendingMigrations(migrations, applied)

	if len(got) != 2 {
		t.Fatalf("len(pending) = %d, want 2", len(got))
	}
	if got[0].Version != "001_first" {
		t.Fatalf("first pending = %q, want %q", got[0].Version, "001_first")
	}
	if got[1].Version != "020_third" {
		t.Fatalf("second pending = %q, want %q", got[1].Version, "020_third")
	}
}

func TestApplyMigrationTxDoesNotRecordFailedMigration(t *testing.T) {
	wantErr := errors.New("boom")
	tx := &fakeMigrationTx{failOnSQL: "broken", err: wantErr}

	err := applyMigrationTx(context.Background(), tx, Migration{
		Version: "001_broken",
		SQL:     "broken",
	})
	if err == nil {
		t.Fatal("applyMigrationTx() error = nil, want error")
	}

	for _, stmt := range tx.statements {
		if strings.Contains(stmt, "INSERT INTO schema_migrations") {
			t.Fatalf("failed migration was recorded: %q", stmt)
		}
	}
	if tx.committed {
		t.Fatal("failed migration was committed")
	}
}

func TestApplyMigrationTxRecordsAfterSuccessfulSQL(t *testing.T) {
	tx := &fakeMigrationTx{}

	err := applyMigrationTx(context.Background(), tx, Migration{
		Version: "001_ok",
		SQL:     "SELECT 1;",
	})
	if err != nil {
		t.Fatalf("applyMigrationTx() error = %v", err)
	}

	if len(tx.statements) != 2 {
		t.Fatalf("len(statements) = %d, want 2", len(tx.statements))
	}
	if !strings.Contains(tx.statements[1], "INSERT INTO schema_migrations") {
		t.Fatalf("second statement = %q, want schema_migrations insert", tx.statements[1])
	}
	if !tx.committed {
		t.Fatal("successful migration was not committed")
	}
}

type fakeMigrationTx struct {
	statements []string
	failOnSQL  string
	err        error
	committed  bool
}

func (tx *fakeMigrationTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.statements = append(tx.statements, sql)
	if sql == tx.failOnSQL {
		return pgconn.CommandTag{}, tx.err
	}
	return pgconn.CommandTag{}, nil
}

func (tx *fakeMigrationTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *fakeMigrationTx) Rollback(context.Context) error {
	return nil
}
