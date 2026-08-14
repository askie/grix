#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INIT_SCHEMA_FILE="${ROOT_DIR}/migration/001_init_schema.sql"

DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-aibot}"
DB_PASSWORD="${DB_PASSWORD:-aibot_secret}"
DB_NAME="${DB_NAME:-aibot}"

REDIS_HOST="${REDIS_HOST:-127.0.0.1}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_DB="${REDIS_DB:-0}"
REDIS_PASSWORD="${REDIS_PASSWORD:-}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-aibot-postgres}"
REDIS_CONTAINER="${REDIS_CONTAINER:-aibot-redis}"
SKIP_REDIS="${SKIP_REDIS:-0}"

run_psql_cmd() {
  local sql="$1"
  if command -v psql >/dev/null 2>&1; then
    psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -c "${sql}"
    return 0
  fi

  if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx "${POSTGRES_CONTAINER}"; then
    docker exec -e PGPASSWORD="${DB_PASSWORD}" "${POSTGRES_CONTAINER}" \
      psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -c "${sql}"
    return 0
  fi

  echo "[dev-reset] ERROR: psql not found and container ${POSTGRES_CONTAINER} not running" >&2
  exit 127
}

run_psql_file() {
  local file="$1"
  if command -v psql >/dev/null 2>&1; then
    psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f "${file}"
    return 0
  fi

  if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx "${POSTGRES_CONTAINER}"; then
    docker exec -i -e PGPASSWORD="${DB_PASSWORD}" "${POSTGRES_CONTAINER}" \
      psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 < "${file}"
    return 0
  fi

  echo "[dev-reset] ERROR: psql not found and container ${POSTGRES_CONTAINER} not running" >&2
  exit 127
}

run_redis() {
  if command -v redis-cli >/dev/null 2>&1; then
    if [[ -n "${REDIS_PASSWORD}" ]]; then
      redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" -a "${REDIS_PASSWORD}" -n "${REDIS_DB}" FLUSHDB
    else
      redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" -n "${REDIS_DB}" FLUSHDB
    fi
    return 0
  fi

  if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx "${REDIS_CONTAINER}"; then
    if [[ -n "${REDIS_PASSWORD}" ]]; then
      docker exec "${REDIS_CONTAINER}" redis-cli -a "${REDIS_PASSWORD}" -n "${REDIS_DB}" FLUSHDB
    else
      docker exec "${REDIS_CONTAINER}" redis-cli -n "${REDIS_DB}" FLUSHDB
    fi
    return 0
  fi

  echo "[dev-reset] ERROR: redis-cli not found and container ${REDIS_CONTAINER} not running" >&2
  exit 127
}

echo "[dev-reset] root=${ROOT_DIR}"
echo "[dev-reset] reset postgres schema: ${DB_HOST}:${DB_PORT}/${DB_NAME}"

export PGPASSWORD="${DB_PASSWORD}"
run_psql_cmd "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"

if [[ ! -f "${INIT_SCHEMA_FILE}" ]]; then
  echo "[dev-reset] ERROR: init schema not found: ${INIT_SCHEMA_FILE}" >&2
  exit 1
fi
echo "[dev-reset] applying ${INIT_SCHEMA_FILE}"
run_psql_file "${INIT_SCHEMA_FILE}"

if [[ "${SKIP_REDIS}" == "1" ]]; then
  echo "[dev-reset] skip redis flush (SKIP_REDIS=1)"
  exit 0
fi

echo "[dev-reset] flush redis db=${REDIS_DB}: ${REDIS_HOST}:${REDIS_PORT}"
run_redis

echo "[dev-reset] done"
