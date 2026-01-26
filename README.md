
# `deviser` – Kubernetes Events Partition Manager

> Efficiently manages daily PostgreSQL partitions for Kubernetes events.

## Key Features

* Long-running daemon for PostgreSQL partition creation.
* Graceful shutdown on `SIGINT`/`SIGTERM`.
* Health server for Kubernetes liveness/readiness probes.
* Embedded SQL migrations via Go `embed` filesystem.
* Automatic partition creation for future days.

## Use Case

Designed to run inside Kubernetes, `deviser` ensures that high-volume cluster events are stored efficiently in PostgreSQL with daily partitions, while providing reliable observability and minimal resource usage.
