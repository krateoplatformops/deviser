# deviser

**deviser** is a PostgreSQL-backed service designed to manage large volumes of Kubernetes event data efficiently. It provides automated **daily partition creation** and **retention/cleanup management** for partitioned tables, ensuring the database remains performant and storage-efficient.

This service is commonly used alongside `events-ingester` and `events-presenter` as part of a scalable Kubernetes event pipeline:

```txt
Kubernetes Events → events-ingester → PostgreSQL → deviser → events-presenter
```

## Overview

`deviser` focuses on:

- Creating daily partitions for event tables
- Enforcing retention policies
- Cleaning up partitions exceeding quota thresholds
- Providing health probes for Kubernetes
- Graceful shutdown support

It ensures that historical event data is properly managed without manual intervention.


## Features

- **Daily Partition Management** – Automatically creates partitions for the next N days
- **Retention Enforcement** – Drops old partitions according to retention policy
- **Quota Management** – Keeps total partition size under a configurable maximum
- **Dry Run Mode** – Test cleanup without deleting data
- **Embedded SQL Assets** – Stores schema and templates inside the binary
- **Health Probes** – Kubernetes-friendly `/livez` and `/readyz` endpoints
- **OpenTelemetry Metrics** – Exports service metrics via OTLP HTTP
- **Graceful Shutdown** – Ensures no partial operations are performed during termination

## Partitioning Model

- Parent table (default: `k8s_events`) is partitioned by day
- Daily partitions are created in advance (configurable, default 7 days)
- Old partitions are dropped automatically based on retention days
- Quota enforcement triggers cleanup if partitions exceed `MaxPartitionsSizeBytes`

### Quota Parameters

| Parameter | Description |
|-----------|-------------|
| `MaxPartitionsSizeBytes` | Maximum allowed total size of all partitions |
| `TriggerRatio` | Fraction of max size to trigger cleanup (e.g., 0.75) |
| `TargetRatio` | Target fraction after cleanup (e.g., 0.60) |
| `DryRun` | If true, cleanup actions are logged but not executed |


## Requirements

- Kubernetes cluster (optional; service can run outside of cluster)
- PostgreSQL database
- Network connectivity to the database
- Appropriate database privileges for partition creation and table management


## Configuration

Configured via environment variables and flags:

| Flag / Env Var | Description | Default |
|----------------|-------------|---------|
| `PORT` | Service port for health probes | `8081` |
| `DEBUG` | Enable verbose logging | `false` |
| `DB_USER` | Database username | — |
| `DB_PASS` | Database password | — |
| `DB_NAME` | Database name | — |
| `DB_HOST` | Database host | `localhost` |
| `DB_PORT` | Database port | `5432` |
| `DB_PARAMS` | Extra DB params (e.g., sslmode, timeout) | — |
| `DB_READY_TIMEOUT` | Max wait for DB readiness | `3m` |
| `DB_PARTITIONS_DAYS` | Number of days ahead to create partitions | `7` |
| `PM_MAX_PARTITIONS_SIZE` | Maximum total size in bytes | `10GB` |
| `PM_RETENTION_DAYS` | Number of days to retain partitions | `2` |
| `PM_TRIGGER_RATIO` | Quota trigger ratio | `0.75` |
| `PM_TARGET_RATIO` | Quota target ratio | `0.60` |
| `PM_DRY_RUN` | Enable dry-run mode | `false` |
| `OTEL_ENABLED` | Enable OpenTelemetry metrics exporter | `true` |
| `OTEL_EXPORT_INTERVAL` | OpenTelemetry metric export interval | `50s` |

OpenTelemetry exporter endpoint/protocol can be controlled with standard environment variables, for example:

- `OTEL_EXPORTER_OTLP_ENDPOINT`
- `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`

## OpenTelemetry Metrics Usage

`deviser` exports metrics with OTLP HTTP when:

- `OTEL_ENABLED=true`
- `OTEL_EXPORTER_OTLP_ENDPOINT` (or `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`) points to an OpenTelemetry Collector

Recommended flow:

1. `deviser` sends metrics to OpenTelemetry Collector (OTLP HTTP, usually port `4318`)
2. Collector exports metrics to Prometheus-compatible backend
3. Visualize in Grafana (or query directly in Prometheus)

Typical Helm config example:

```yaml
config:
  OTEL_ENABLED: "true"
  OTEL_EXPORT_INTERVAL: "50s"
  OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-collector.monitoring.svc.cluster.local:4318"
```

Some useful metric names to chart:

- `deviser.startup.success`
- `deviser.startup.failure`
- `deviser.db.connect.duration_seconds`
- `deviser.db.schema_apply.duration_seconds`
- `deviser.partitions.ensure.duration_seconds`
- `deviser.partitions.maintain.duration_seconds`
- `deviser.resources.purge.duration_seconds`
- `deviser.resources.purge.rows`
- `deviser.loop.iteration.success`
- `deviser.loop.iteration.failure`

Notes:

- If `OTEL_ENABLED=false`, no metrics are exported.
- This service does not expose a Prometheus `/metrics` endpoint directly; metrics are exported via OTLP.


## How It Works

1. On startup, waits for PostgreSQL to be ready
2. Executes initial schema SQL (`schema.sql`)
3. Loads SQL templates (`partition.tpl.sql`) for partition management
4. Launches:

   - **Daily partition creation loop** (runs every hour)
   - **Partition manager** to drop expired partitions and enforce size quotas
   - **Health probe server** on configured port

5. On shutdown:

   - Stops loops gracefully
   - Closes DB connections
   - Exits cleanly


## Health Probes

| Endpoint | Purpose |
|----------|---------|
| `/livez` | Liveness probe (service is running) |
| `/readyz` | Readiness probe (database reachable, partitions ready) |

> These endpoints are suitable for Kubernetes `livenessProbe` and `readinessProbe`.


## Deployment Notes

- Store database credentials in Kubernetes Secrets
- Set resource limits for CPU and memory
- Enable dry-run mode first to validate partition logic
- Adjust `DB_PARTITIONS_DAYS`, `PM_RETENTION_DAYS`, and quota parameters according to cluster size and database capacity
- Recommended to run alongside `events-ingester` and `events-presenter` for full event pipeline


## Example Partition Flow

```txt
Parent Table: k8s_events
Daily Partitions:
k8s_events_2026_02_13
k8s_events_2026_02_14
k8s_events_2026_02_15
Retention: 2 days
Quota: 10 GB
Cleanup triggered at 75%, target 60%
```
