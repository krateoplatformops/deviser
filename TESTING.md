# Testing deviser

This document describes the test layers of **deviser**, how to run them locally, and the conventions to follow when adding new tests.

## Overview

| Layer | What it covers | Requirements | Typical runtime |
|---|---|---|---|
| Unit tests | Pure logic: migration ordering/recording, partition bound parsing, asset loading | Go only | < 1s |
| Integration tests (`TestBYOPostgres`) | Full database lifecycle against real PostgreSQL 13/17/18, including the documented Bring Your Own PostgreSQL provisioning | Go + Docker | ~40s (cached images) |
| End-to-end (`scripts/e2e.sh`) | Container image + Helm chart + live service against PostgreSQL inside a kind cluster | Docker, kind, kubectl, helm, curl | a few minutes |
| CI (`.github/workflows/test.yml`) | `go vet` + unit + integration on every PR and push to `main` | — | — |

## Prerequisites

- **Go** — version pinned in [`go.mod`](go.mod) (toolchain auto-downloads if needed).
- **Docker** — required only for the integration tests (they use [testcontainers-go](https://golang.testcontainers.org/) to start disposable PostgreSQL containers) and for the e2e script. Without a running Docker daemon, integration tests **skip automatically** — they never fail for that reason.

## Running tests

### Everything

```sh
go vet ./...
go test ./... -count=1
```

`-count=1` bypasses Go's test result cache. Use it whenever you changed SQL assets, fixtures, or anything outside `.go` files — cached results can otherwise mask a regression.

### Unit tests only

Either stop Docker (integration tests skip), or skip them explicitly:

```sh
go test ./... -skip 'TestBYOPostgres'
```

### Integration tests only

```sh
go test ./internal/pg -run TestBYOPostgres -count=1 -v
```

The first run pulls the `postgres:12-alpine`, `postgres:13-alpine`, `postgres:17.2-alpine`, `postgres:17-alpine` and `postgres:18-alpine` images; subsequent runs reuse them. To pre-pull:

```sh
docker pull postgres:12-alpine postgres:13-alpine postgres:17.2-alpine postgres:17-alpine postgres:18-alpine
```

A single version or scenario can be selected with the subtest path:

```sh
go test ./internal/pg -run 'TestBYOPostgres/postgres:17-alpine' -count=1 -v
go test ./internal/pg -run 'TestBYOPostgres/postgres:17-alpine/db-owner' -count=1 -v
```

## Test layout

```
internal/config/migrations_test.go     embedded migration asset discovery and ordering
internal/pg/migrations_test.go         migration apply/record logic (fake transaction, no DB)
internal/pg/partitions_test.go         partition helpers
internal/pg/partition_manager_test.go  partition bound parsing
internal/pg/byo_test.go                integration: full lifecycle on real PostgreSQL + minimum-version guard
internal/pg/testdata/byo_*.sql         provisioning fixtures for the BYO scenarios
```

## The integration test: `TestBYOPostgres`

`internal/pg/byo_test.go` verifies that the **exact provisioning SQL documented in the Bring Your Own PostgreSQL guide** is sufficient — and minimal — for everything deviser does at runtime. It runs a 4×3 matrix (versions × scenarios):

### PostgreSQL versions

| Image | Why |
|---|---|
| `postgres:13-alpine` | Minimum supported version (first with native `gen_random_uuid()`) |
| `postgres:17.2-alpine` | Pinned patch release, exact-version coverage |
| `postgres:17-alpine` | Latest 17.x patch release |
| `postgres:18-alpine` | CNPG default version |

One container is started per version; each scenario provisions its own role and database inside it.

### Scenarios

| Scenario | Provisioning | Expectation |
|---|---|---|
| `db-owner` | Verbatim guide SQL (`testdata/byo_db_owner_setup.sql`): application user **owns** the database, hyphenated identifiers | Full lifecycle passes |
| `schema-grants` | Least-privilege alternative (`testdata/byo_schema_grants_*.sql`): `CONNECT` + `USAGE, CREATE ON SCHEMA public`, no ownership | Full lifecycle passes **and** `CREATE EXTENSION` is denied — proves deviser needs neither superuser nor extension privileges |
| `grant-all-only` | Legacy model: `GRANT ALL PRIVILEGES ON DATABASE` only | Bootstrap **fails** with SQLSTATE `42501` on PostgreSQL ≥ 15 (public schema no longer world-writable); still passes on 13/14. Pins the rationale for the documented setup |

### Full lifecycle steps

Each passing scenario executes, **as the application role only**, mirroring `main.go` order and using the production code paths:

1. Connect via `pgutil.ConnectionURL` + `pgutil.WaitForPostgres` (exercises URL building with hyphenated credentials)
2. Apply both schemas (`k8s_events.schema.sql`, `resources.schema.sql`)
3. Apply embedded migrations (`ApplyMigrations`, recorded in `schema_migrations`)
4. Create daily partitions via the real `CreateDailyPartitions`
5. `LISTEN events` → insert an event → assert the trigger notification carries the database-generated `event_id` and `global_uid`
6. Read/write `krateo_resources` (what the ingester/presenter components do)
7. Purge soft-deleted resources (`PurgeDeletedResources`)
8. Drop an expired partition via `PartitionManager.Maintain`
9. Re-apply schemas and migrations (restart idempotency)
10. Assert `pgcrypto` is **not** installed (guards against reintroducing the extension dependency)

### Below-minimum version guard (must fail)

`TestBYOPostgresBelowMinimumVersion` runs the schema bootstrap on `postgres:12-alpine` (the latest and final 12.x patch release, EOL) and asserts it **fails** with SQLSTATE `42883`: `gen_random_uuid()` is not a core function before PostgreSQL 13. It provisions with the documented db-owner setup on purpose, so the failure is attributable to the missing function and not to privileges. If this test ever starts passing, the documented minimum version claim must be revisited.

```sh
go test ./internal/pg -run TestBYOPostgresBelowMinimumVersion -count=1 -v
```

### Keeping fixtures in sync with the documentation

`testdata/byo_db_owner_setup.sql` must stay **verbatim identical** to step 1 of the guide in the `krateo-v2-docs` repository:

```
docs/30-how-to-guides/50-manage-postgresql/40-bring-your-own-postgresql.md
```

If the guide changes, update the fixture (and vice versa). The fixture header comment carries the same pointer.

## End-to-end: `scripts/e2e.sh`

Builds the local image, loads it into a [kind](https://kind.sigs.k8s.io/) cluster, deploys PostgreSQL (`scripts/postgres.yaml`) and the deviser Helm chart, then asserts `/readyz` and the schema/migration state inside the database.

```sh
POSTGRES_NAMESPACE=demo-system ./scripts/e2e.sh
```

> **Note:** `scripts/postgres.yaml` hardcodes the `demo-system` namespace, while the script's `POSTGRES_NAMESPACE` defaults to `test-system` — pass `POSTGRES_NAMESPACE=demo-system` explicitly (as above) until the default is aligned.

Useful environment overrides (see the script header for the full list): `CLUSTER_NAME`, `IMAGE`, `APP_NAMESPACE`, `DB_USER`, `DB_PASS`, `DB_NAME`, `LOCAL_PORT`.

Teardown:

```sh
./scripts/kind-down.sh
```

## Continuous integration

[`.github/workflows/test.yml`](.github/workflows/test.yml) runs `go vet ./...` and `go test ./... -count=1 -timeout 20m` on every pull request and push to `main`. GitHub-hosted runners include Docker, so the full integration matrix runs in CI as well.

## Conventions for new tests

- **Prefer table-driven unit tests** with the standard library only — no assertion frameworks (see `partition_manager_test.go`).
- **Fakes over mocks**: for code that talks to the database through a narrow interface, add a small in-package fake (see `fakeMigrationTx` in `migrations_test.go`) instead of spinning up a container.
- **Reach for testcontainers only when behavior depends on real PostgreSQL** (DDL, privileges, triggers, catalog queries). Reuse the helpers in `byo_test.go` (`mustConnectionURL`, `execStatements`, `listPartitionNames`, …) rather than duplicating setup.
- Integration tests must **skip, not fail, without Docker**: call `testcontainers.SkipIfProviderIsNotHealthy(t)` first.
- **Exercise production code paths** in integration tests (real `CreateDailyPartitions`, `ApplyMigrations`, `pgutil` helpers) instead of re-implementing their SQL. `*telemetry.Metrics` is nil-safe, so a `nil` metrics value is fine in tests.
- Provisioning SQL that mirrors public documentation belongs in `testdata/*.sql` with a header comment pointing at the doc — never inline it where it can drift silently.
- Multi-statement provisioning scripts are split and executed one statement at a time (`CREATE DATABASE` cannot run inside the implicit transaction of a multi-statement `Exec`); keep one statement per `;` and comments on their own lines.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `SKIP ... Docker is not running` | Start Docker Desktop (or your daemon). Integration tests skip by design without it. |
| Test reports `(cached)` and ignores your change | Re-run with `-count=1`. |
| First integration run is slow or times out | Image pulls. Pre-pull the five `postgres:*-alpine` images, or raise `-timeout`. |
| `permission denied for schema public` in a new scenario | Expected on PostgreSQL ≥ 15 unless the role owns the database or has `CREATE` on the schema — see the `grant-all-only` scenario. |
| e2e script waits forever on the PostgreSQL rollout | Namespace mismatch — run with `POSTGRES_NAMESPACE=demo-system`. |
