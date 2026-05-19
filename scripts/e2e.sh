#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

CLUSTER_NAME="${CLUSTER_NAME:-kind}"
POSTGRES_NAMESPACE="${POSTGRES_NAMESPACE:-test-system}"
POSTGRES_DEPLOYMENT="${POSTGRES_DEPLOYMENT:-postgres}"
APP_NAMESPACE="${APP_NAMESPACE:-test-system}"
RELEASE_NAME="${RELEASE_NAME:-deviser}"
IMAGE="${IMAGE:-deviser:e2e}"
LOCAL_PORT="${LOCAL_PORT:-18081}"

DB_USER="${DB_USER:-test}"
DB_PASS="${DB_PASS:-test}"
DB_NAME="${DB_NAME:-testdb}"
DB_HOST="${DB_HOST:-postgres.${POSTGRES_NAMESPACE}.svc.cluster.local}"

log() {
  printf '\n==> %s\n' "$*"
}

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

cleanup() {
  if [[ -n "${PORT_FORWARD_PID:-}" ]]; then
    kill "${PORT_FORWARD_PID}" >/dev/null 2>&1 || true
    wait "${PORT_FORWARD_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

wait_for_http() {
  local url="$1"
  local attempts="${2:-30}"

  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  printf 'timeout waiting for %s\n' "$url" >&2
  return 1
}

need kind
need kubectl
need docker
need helm
need curl

cd "${ROOT_DIR}"

if ! kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
  log "kind cluster '${CLUSTER_NAME}' not found; starting it"
  "${ROOT_DIR}/scripts/kind-up.sh"
else
  log "kind cluster '${CLUSTER_NAME}' already running"
fi

log "installing PostgreSQL"
kubectl apply -f "${ROOT_DIR}/scripts/postgres.yaml"
kubectl -n "${POSTGRES_NAMESPACE}" rollout status "deployment/${POSTGRES_DEPLOYMENT}" --timeout=180s

log "building local image ${IMAGE}"
docker build -t "${IMAGE}" "${ROOT_DIR}"

log "loading image into kind"
kind load docker-image "${IMAGE}" --name "${CLUSTER_NAME}"

log "deploying deviser"
kubectl get namespace "${APP_NAMESPACE}" >/dev/null 2>&1 || kubectl create namespace "${APP_NAMESPACE}"

helm template "${RELEASE_NAME}" "${ROOT_DIR}/chart" \
  -n "${APP_NAMESPACE}" \
  -f "${ROOT_DIR}/chart/values.dev.yaml" \
  --set image.repository="${IMAGE%:*}" \
  --set image.tag="${IMAGE##*:}" \
  --set image.pullPolicy=Never \
  --set config.DB_HOST="${DB_HOST}" \
  --set config.DB_PORT="5432" \
  --set config.DB_NAME="${DB_NAME}" \
  --set config.DB_USER="${DB_USER}" \
  --set config.DB_PASS="${DB_PASS}" \
  --set config.DB_PARAMS="sslmode=disable&connect_timeout=5" \
  --set config.OTEL_ENABLED="false" \
  | kubectl apply -f -

kubectl -n "${APP_NAMESPACE}" rollout restart "deployment/${RELEASE_NAME}"
kubectl -n "${APP_NAMESPACE}" rollout status "deployment/${RELEASE_NAME}" --timeout=180s

log "checking health endpoint"
kubectl -n "${APP_NAMESPACE}" port-forward "svc/${RELEASE_NAME}" "${LOCAL_PORT}:8081" >/tmp/deviser-e2e-port-forward.log 2>&1 &
PORT_FORWARD_PID="$!"
wait_for_http "http://127.0.0.1:${LOCAL_PORT}/readyz" 60

log "checking database schema and migration state"
kubectl -n "${POSTGRES_NAMESPACE}" exec -i "deployment/${POSTGRES_DEPLOYMENT}" -- \
  psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_name = 'k8s_events'
      AND column_name = 'event_id'
      AND is_nullable = 'NO'
  ) THEN
    RAISE EXCEPTION 'missing NOT NULL k8s_events.event_id column';
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM schema_migrations
    WHERE version = '001_k8s_events_event_id'
  ) THEN
    RAISE EXCEPTION 'missing schema_migrations record for 001_k8s_events_event_id';
  END IF;

  IF pg_get_functiondef('notify_new_event'::regproc) NOT LIKE '%json_build_object%event_id%global_uid%' THEN
    RAISE EXCEPTION 'notify_new_event does not publish JSON event_id/global_uid payload';
  END IF;
END $$;
SQL

log "e2e completed successfully"
printf 'deviser ready: http://127.0.0.1:%s/readyz\n' "${LOCAL_PORT}"
