package pg

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krateoplatformops/deviser/internal/config"
	"github.com/krateoplatformops/plumbing/pgutil"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Scenario "db-owner": the documented BYO setup, verbatim from the guide.
//
//go:embed testdata/byo_db_owner_setup.sql
var byoDbOwnerSetupSQL string

// Scenario "schema-grants": least-privilege alternative, test-only evidence.
//
//go:embed testdata/byo_schema_grants_setup.sql
var byoSchemaGrantsSetupSQL string

//go:embed testdata/byo_schema_grants_grants.sql
var byoSchemaGrantsGrantsSQL string

// Scenario "grant-all-only": the PREVIOUSLY documented setup, kept as a
// negative case. On PostgreSQL 15+ the public schema is no longer
// world-writable, so "GRANT ALL PRIVILEGES ON DATABASE" alone is NOT enough
// for deviser's first CREATE TABLE. This scenario pins the reason why the
// guide now makes the application user the owner of the database.
const byoGrantAllOnlySetupSQL = `
CREATE ROLE krateo_app_ga LOGIN PASSWORD 'krateo_app_ga' NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE DATABASE krateo_db_ga;
GRANT ALL PRIVILEGES ON DATABASE krateo_db_ga TO krateo_app_ga;
`

// TestBYOPostgres verifies the Bring Your Own PostgreSQL scenario end to end:
// the exact provisioning SQL from the guide must be sufficient (and minimal)
// for the full deviser lifecycle, executed as the application role only:
// schema bootstrap, migrations, daily partition creation, notify trigger,
// resources read/write, soft-delete purge, partition retention drop and
// restart idempotency. No superuser and no extension privileges involved.
func TestBYOPostgres(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	images := []string{
		"postgres:13-alpine", // minimum supported version (first with native gen_random_uuid)
		"postgres:17-alpine", // reference version for external/bring-your-own PostgreSQL
		"postgres:18-alpine", // CNPG default version
	}

	for _, image := range images {
		t.Run(image, func(t *testing.T) {
			byoScenarios(t, image)
		})
	}
}

func byoScenarios(t *testing.T, image string) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	mappedPort, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("container mapped port: %v", err)
	}
	port := mappedPort.Int()

	adminPool, err := pgxpool.New(ctx, mustConnectionURL(t, "postgres", "postgres", host, port, "postgres"))
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer adminPool.Close()

	serverMajor := serverMajorVersion(ctx, t, adminPool)

	t.Run("db-owner", func(t *testing.T) {
		execStatements(ctx, t, adminPool, byoDbOwnerSetupSQL)

		appURL := mustConnectionURL(t, "krateo-db-user", "your_password", host, port, "krateo-db")
		runFullLifecycle(ctx, t, appURL, lifecycleOpts{})
	})

	t.Run("schema-grants", func(t *testing.T) {
		execStatements(ctx, t, adminPool, byoSchemaGrantsSetupSQL)

		// Schema-level grants must be issued from inside the target database.
		adminDbPool, err := pgxpool.New(ctx, mustConnectionURL(t, "postgres", "postgres", host, port, "krateo_db_lp"))
		if err != nil {
			t.Fatalf("connect as admin to krateo_db_lp: %v", err)
		}
		defer adminDbPool.Close()
		execStatements(ctx, t, adminDbPool, byoSchemaGrantsGrantsSQL)

		appURL := mustConnectionURL(t, "krateo_app_lp", "krateo_app_lp", host, port, "krateo_db_lp")
		runFullLifecycle(ctx, t, appURL, lifecycleOpts{assertNoExtensionPrivilege: true})
	})

	t.Run("grant-all-only", func(t *testing.T) {
		execStatements(ctx, t, adminPool, byoGrantAllOnlySetupSQL)

		appURL := mustConnectionURL(t, "krateo_app_ga", "krateo_app_ga", host, port, "krateo_db_ga")
		runGrantAllOnlyBootstrap(ctx, t, appURL, serverMajor)
	})
}

type lifecycleOpts struct {
	// assertNoExtensionPrivilege additionally proves the role cannot run
	// CREATE EXTENSION at all, so the bootstrap demonstrably requires none.
	assertNoExtensionPrivilege bool
}

// runFullLifecycle executes every database operation of the deviser lifecycle
// (plus the write/read patterns of the ingester/presenter components) as the
// application role only, mirroring the order of main.go.
func runFullLifecycle(ctx context.Context, t *testing.T, appURL string, opts lifecycleOpts) {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{Log: log}

	// Connect exactly like main.go does.
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	pool, err := pgutil.WaitForPostgres(waitCtx, log, appURL)
	if err != nil {
		t.Fatalf("connect as application role: %v", err)
	}
	defer pool.Close()

	var super bool
	if err := pool.QueryRow(ctx,
		`SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&super); err != nil {
		t.Fatalf("check role privileges: %v", err)
	}
	if super {
		t.Fatal("application role is superuser; test would not prove anything")
	}

	if opts.assertNoExtensionPrivilege {
		if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err == nil {
			t.Fatal("application role can install extensions; least-privilege setup is broken")
		}
	}

	// 1-2. Schema bootstrap + migrations (idempotent, like a service start).
	bootstrap := func() {
		applySchemas(ctx, t, pool, cfg)
		applyTestMigrations(ctx, t, pool, cfg, log)
	}
	bootstrap()

	// 3. Daily partitions via the real production code path.
	tpl, err := cfg.LoadSQLTemplate("partition.tpl.sql", "partition")
	if err != nil {
		t.Fatalf("load partition template: %v", err)
	}
	if err := CreateDailyPartitions(ctx, &CreateDailyPartitionsOptions{
		Pool: pool,
		Log:  log,
		Tpl:  tpl,
		Days: 3,
	}); err != nil {
		t.Fatalf("create daily partitions as application role: %v", err)
	}

	// Extra expired partition to exercise the retention drop below.
	oldDay := startOfUTCDay(time.Now()).AddDate(0, 0, -10)
	oldPartition := createPartitionFor(ctx, t, pool, tpl, oldDay)

	if names := listPartitionNames(ctx, t, pool); len(names) != 4 {
		t.Fatalf("partitions after creation = %v, want 4 (3 daily + 1 expired)", names)
	}

	// 4. The notify trigger must fire with a database-generated event_id.
	listenConn, err := pgx.Connect(ctx, appURL)
	if err != nil {
		t.Fatalf("open LISTEN connection: %v", err)
	}
	defer listenConn.Close(ctx)
	if _, err := listenConn.Exec(ctx, `LISTEN events`); err != nil {
		t.Fatalf("LISTEN events: %v", err)
	}

	var eventID string
	err = pool.QueryRow(ctx, `
INSERT INTO k8s_events (
    created_at, cluster_name, uid, global_uid,
    namespace, resource_kind, resource_name,
    event_type, raw, resource_version
) VALUES (
    $1, 'test-cluster', 'uid-1', 'test-cluster:uid-1',
    'default', 'Pod', 'test-pod',
    'Normal', '{}'::jsonb, '1'
) RETURNING event_id::text`, time.Now().UTC()).Scan(&eventID)
	if err != nil {
		t.Fatalf("insert event as application role: %v", err)
	}
	if len(eventID) != 36 {
		t.Fatalf("event_id = %q, want database-generated UUID", eventID)
	}

	notifyCtx, cancelNotify := context.WithTimeout(ctx, 10*time.Second)
	defer cancelNotify()
	notification, err := listenConn.WaitForNotification(notifyCtx)
	if err != nil {
		t.Fatalf("wait for trigger notification: %v", err)
	}
	var payload struct {
		EventID   string `json:"event_id"`
		GlobalUID string `json:"global_uid"`
	}
	if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
		t.Fatalf("decode notification payload %q: %v", notification.Payload, err)
	}
	if payload.EventID != eventID || payload.GlobalUID != "test-cluster:uid-1" {
		t.Fatalf("notification payload = %+v, want event_id=%s global_uid=test-cluster:uid-1",
			payload, eventID)
	}

	// 5. krateo_resources read/write, as the ingester/presenter components do.
	var resourceID int64
	err = pool.QueryRow(ctx, `
INSERT INTO krateo_resources (
    cluster_name, uid, global_uid, namespace,
    resource_group, resource_version, resource_kind,
    resource_plural, resource_name, raw
) VALUES (
    'test-cluster', 'res-1', 'test-cluster:res-1', 'default',
    'apps', 'v1', 'Deployment',
    'deployments', 'app-1', '{}'::jsonb
) RETURNING id`).Scan(&resourceID)
	if err != nil {
		t.Fatalf("insert resource: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE krateo_resources SET status_raw = '{"ready":true}'::jsonb, updated_at = now() WHERE id = $1`,
		resourceID); err != nil {
		t.Fatalf("update resource: %v", err)
	}

	// 6. Soft-delete purge needs DELETE privileges.
	if _, err := pool.Exec(ctx, `
INSERT INTO krateo_resources (
    cluster_name, uid, global_uid, namespace,
    resource_group, resource_version, resource_kind,
    resource_plural, resource_name, raw, deleted_at
) VALUES (
    'test-cluster', 'res-2', 'test-cluster:res-2', 'default',
    'apps', 'v1', 'Deployment',
    'deployments', 'app-2', '{}'::jsonb, now() - interval '40 days'
)`); err != nil {
		t.Fatalf("insert soft-deleted resource: %v", err)
	}

	purged, err := PurgeDeletedResources(ctx, &PurgeDeletedResourcesOptions{
		Pool:          pool,
		Log:           log,
		RetentionDays: 30,
		BatchSize:     100,
	})
	if err != nil {
		t.Fatalf("purge soft-deleted resources: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want 1", purged)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM krateo_resources`).Scan(&remaining); err != nil {
		t.Fatalf("count resources: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining resources = %d, want 1", remaining)
	}

	// 7. Partition maintenance needs DROP TABLE on expired partitions.
	pm := &PartitionManager{
		Pool:          pool,
		Log:           log,
		ParentTable:   "k8s_events",
		RetentionDays: 2,
	}
	if err := pm.Maintain(ctx); err != nil {
		t.Fatalf("partition maintenance as application role: %v", err)
	}
	names := listPartitionNames(ctx, t, pool)
	for _, name := range names {
		if name == oldPartition {
			t.Fatalf("expired partition %s was not dropped", oldPartition)
		}
	}
	if len(names) != 3 {
		t.Fatalf("partitions after maintenance = %v, want the 3 current daily partitions", names)
	}

	// 8. Restart idempotency: re-applying schemas and migrations must succeed.
	bootstrap()

	// 9. Guard against reintroducing the extension dependency: the lifecycle
	// must work on a database where pgcrypto was never installed.
	var pgcryptoInstalled bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto')`).Scan(&pgcryptoInstalled); err != nil {
		t.Fatalf("check pg_extension: %v", err)
	}
	if pgcryptoInstalled {
		t.Fatal("pgcrypto extension is installed; deviser must not depend on it")
	}
}

// runGrantAllOnlyBootstrap documents that the legacy provisioning model is
// insufficient on PostgreSQL 15+ (and still works on 13/14, where the public
// schema is world-writable).
func runGrantAllOnlyBootstrap(ctx context.Context, t *testing.T, appURL string, serverMajor int) {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{Log: log}

	pool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("connect as application role: %v", err)
	}
	defer pool.Close()

	sql, err := cfg.LoadSQL("k8s_events.schema.sql")
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	_, err = pool.Exec(ctx, sql)

	if serverMajor >= 15 {
		if err == nil {
			t.Fatal("schema bootstrap succeeded; GRANT ALL PRIVILEGES ON DATABASE should NOT be sufficient on PostgreSQL 15+")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" { // insufficient_privilege
			t.Fatalf("schema bootstrap error = %v, want SQLSTATE 42501 (permission denied for schema public)", err)
		}
		return
	}

	// PostgreSQL 13/14: public schema is world-writable, legacy grants are
	// still enough to complete the bootstrap.
	if err != nil {
		t.Fatalf("apply schema on PostgreSQL <15: %v", err)
	}
	applySchemas(ctx, t, pool, cfg)
	applyTestMigrations(ctx, t, pool, cfg, log)
}

func applySchemas(ctx context.Context, t *testing.T, pool *pgxpool.Pool, cfg *config.Config) {
	t.Helper()
	for _, schema := range []string{"k8s_events.schema.sql", "resources.schema.sql"} {
		sql, err := cfg.LoadSQL(schema)
		if err != nil {
			t.Fatalf("load schema %s: %v", schema, err)
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply schema %s as application role: %v", schema, err)
		}
	}
}

func applyTestMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) {
	t.Helper()
	assets, err := cfg.LoadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	migrations := make([]Migration, 0, len(assets))
	for _, asset := range assets {
		migrations = append(migrations, Migration{
			Version: asset.Version,
			SQL:     asset.SQL,
		})
	}
	if err := ApplyMigrations(ctx, pool, log, migrations); err != nil {
		t.Fatalf("apply migrations as application role: %v", err)
	}
}

func createPartitionFor(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tpl *template.Template, day time.Time) string {
	t.Helper()
	name := fmt.Sprintf("k8s_events_%04d_%02d_%02d", day.Year(), day.Month(), day.Day())
	var sb strings.Builder
	if err := tpl.Execute(&sb, map[string]string{
		"PartitionName": name,
		"StartDate":     day.Format(partitionTimestampLayout),
		"EndDate":       day.AddDate(0, 0, 1).Format(partitionTimestampLayout),
	}); err != nil {
		t.Fatalf("render partition template: %v", err)
	}
	if _, err := pool.Exec(ctx, sb.String()); err != nil {
		t.Fatalf("create partition %s: %v", name, err)
	}
	return name
}

func listPartitionNames(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT c.relname
FROM pg_class c
JOIN pg_inherits i ON i.inhrelid = c.oid
WHERE i.inhparent = 'k8s_events'::regclass`)
	if err != nil {
		t.Fatalf("list partitions: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan partition name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate partitions: %v", err)
	}
	return names
}

func mustConnectionURL(t *testing.T, user, pass, host string, port int, dbname string) string {
	t.Helper()
	// Build the URL with the same production code used by deviser's config.
	u, err := pgutil.ConnectionURL(user, pass, host, port, dbname, map[string]string{"sslmode": "disable"})
	if err != nil {
		t.Fatalf("build connection URL: %v", err)
	}
	return u
}

func serverMajorVersion(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&n); err != nil {
		t.Fatalf("read server version: %v", err)
	}
	return n / 10000
}

func execStatements(ctx context.Context, t *testing.T, pool *pgxpool.Pool, script string) {
	t.Helper()
	for _, stmt := range sqlStatements(script) {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup statement %q: %v", stmt, err)
		}
	}
}

// sqlStatements splits a provisioning script into single statements:
// CREATE DATABASE cannot run inside the implicit transaction that wraps a
// multi-statement Exec, so each statement is executed on its own.
// Comment lines are stripped first, so they may freely contain semicolons.
func sqlStatements(script string) []string {
	var sb strings.Builder
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		sb.WriteString(trimmed)
		sb.WriteString("\n")
	}

	var stmts []string
	for _, chunk := range strings.Split(sb.String(), ";") {
		if stmt := strings.TrimSpace(chunk); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}
