# Deviser Metrics Reference

This document describes the OpenTelemetry metrics emitted by `deviser`.

## Naming note

In code, metric names use dots (for example `deviser.startup.success`).
In Prometheus, names are typically normalized with underscores (for example `deviser_startup_success`), and counters may be exposed with `_total`.

## Metrics

| Metric | Type | Unit | Description | Emitted from | PromQL example |
|---|---|---|---|---|---|
| `deviser.startup.success` | Counter | count | Service startup completed successfully. | `main.go` | `sum(increase(deviser_startup_success_total[1h]))` |
| `deviser.startup.failure` | Counter | count | Service startup failed. | `main.go` | `sum(increase(deviser_startup_failure_total[1h]))` |
| `deviser.db.connect.duration_seconds` | Histogram | seconds | Time spent waiting for PostgreSQL readiness. | `main.go` | `histogram_quantile(0.95, sum by (le) (rate(deviser_db_connect_duration_seconds_bucket[5m])))` |
| `deviser.db.schema_apply.duration_seconds` | Histogram | seconds | Time spent applying startup SQL schemas. | `main.go` | `histogram_quantile(0.95, sum by (le) (rate(deviser_db_schema_apply_duration_seconds_bucket[5m])))` |
| `deviser.db.schema_apply.failure` | Counter | count | Failures during schema application. | `main.go` | `sum(increase(deviser_db_schema_apply_failure_total[1h]))` |
| `deviser.partitions.ensure.duration_seconds` | Histogram | seconds | Duration of daily partition creation/ensure routine. | `internal/pg/partitions.go` | `histogram_quantile(0.95, sum by (le) (rate(deviser_partitions_ensure_duration_seconds_bucket[5m])))` |
| `deviser.partitions.ensure.failure` | Counter | count | Failures in partition ensure routine. | `internal/pg/partitions.go` | `sum(increase(deviser_partitions_ensure_failure_total[1h]))` |
| `deviser.partitions.ensure.days` | Gauge | days | Configured horizon (days ahead) for partition creation. | `internal/pg/partitions.go` | `max(deviser_partitions_ensure_days)` |
| `deviser.partitions.maintain.duration_seconds` | Histogram | seconds | Duration of partition maintenance routine. | `internal/pg/partition_manager.go` | `histogram_quantile(0.95, sum by (le) (rate(deviser_partitions_maintain_duration_seconds_bucket[5m])))` |
| `deviser.partitions.maintain.failure` | Counter | count | Failures in partition maintenance. | `internal/pg/partition_manager.go` | `sum(increase(deviser_partitions_maintain_failure_total[1h]))` |
| `deviser.partitions.dropped.expired` | Counter | count | Partitions dropped due to retention policy. | `internal/pg/partition_manager.go` | `sum(increase(deviser_partitions_dropped_expired_total[1h]))` |
| `deviser.partitions.dropped.quota` | Counter | count | Partitions dropped due to quota enforcement. | `internal/pg/partition_manager.go` | `sum(increase(deviser_partitions_dropped_quota_total[1h]))` |
| `deviser.partitions.bytes_freed` | Counter | bytes | Bytes reclaimed by dropping partitions. | `internal/pg/partition_manager.go` | `sum(increase(deviser_partitions_bytes_freed_total[1h]))` |
| `deviser.partitions.total_discovered` | Gauge | count | Number of partitions discovered during maintenance. | `internal/pg/partition_manager.go` | `max(deviser_partitions_total_discovered)` |
| `deviser.partitions.total_bytes` | Gauge | bytes | Total size in bytes of discovered partitions. | `internal/pg/partition_manager.go` | `max(deviser_partitions_total_bytes)` |
| `deviser.resources.purge.duration_seconds` | Histogram | seconds | Duration of soft-delete purge routine. | `internal/pg/purge.go` | `histogram_quantile(0.95, sum by (le) (rate(deviser_resources_purge_duration_seconds_bucket[5m])))` |
| `deviser.resources.purge.rows` | Counter | rows | Number of rows hard-deleted from `krateo_resources`. | `internal/pg/purge.go` | `sum(increase(deviser_resources_purge_rows_total[1h]))` |
| `deviser.resources.purge.failure` | Counter | count | Failures in soft-delete purge routine. | `internal/pg/purge.go` | `sum(increase(deviser_resources_purge_failure_total[1h]))` |
| `deviser.loop.iteration.success` | Counter | count | Main scheduled loop iteration with no errors. | `main.go` | `sum(rate(deviser_loop_iteration_success_total[5m]))` |
| `deviser.loop.iteration.failure` | Counter | count | Main scheduled loop iteration with at least one error. | `main.go` | `sum(rate(deviser_loop_iteration_failure_total[5m]))` |

## Cardinality guidance

- Do not add high-cardinality labels (`uid`, `resource_name`, dynamic IDs).
- Keep metrics service-level and low-cardinality.
- Add labels only when they have clear operational value and bounded cardinality.
