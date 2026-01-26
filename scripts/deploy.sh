#!/bin/bash

set -euo pipefail

# --- Directory dello script e del progetto ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CHART_DIR="$MOD_DIR/chart"

# --- Nome dell'immagine (da esportare per ko) ---
export KO_DOCKER_REPO=kind.local/deviser

# --- Build immagine con ko ---
ko build --base-import-paths "$MOD_DIR"

# --- Carica l'immagine nel cluster kind ---
kind load docker-image "$KO_DOCKER_REPO:latest"

# --- Applicazione del manifest di Postgres ---
kubectl apply -f "$SCRIPT_DIR/postgres.yaml"

# --- Deploy di Helm ---
helm template deviser "$CHART_DIR" -f "$CHART_DIR/values.dev.yaml" | kubectl apply -f -
