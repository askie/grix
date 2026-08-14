#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Sync web bundle unless SKIP_WEB_BUILD=1
if [ "${SKIP_WEB_BUILD}" = "1" ]; then
  echo "[dev-up] skip sync-web-dist (SKIP_WEB_BUILD=1)"
else
  "${ROOT_DIR}/scripts/sync_web_dist.sh"
fi

# Load .env file if present (developer-local overrides)
ENV_FILE="${ROOT_DIR}/.env"
if [ -f "${ENV_FILE}" ]; then
  echo "[dev-up] loading env from ${ENV_FILE}"
  set -a
  # shellcheck source=/dev/null
  . "${ENV_FILE}"
  set +a
fi

RUN_DIR="${ROOT_DIR}/.run"
PID_DIR="${RUN_DIR}/pids"
LOG_DIR="${RUN_DIR}/logs"

GO_BIN="${GO_BIN:-go}"
CONFIG_PATH="${CONFIG_PATH:-config.yaml}"

API_PORT="${API_PORT:-27180}"
WS_PORT="${WS_PORT:-27189}"
LLM_PORT="${LLM_PORT:-27182}"
PUSH_PORT="${PUSH_PORT:-27183}"
GATEWAY_PORT="${GATEWAY_PORT:-27184}"
WEB_PORT="${WEB_PORT:-34123}"

normalize_origin() {
  local raw="${1:-}"
  local trimmed
  trimmed="$(echo "$raw" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//')"
  trimmed="${trimmed%/}"
  echo "$trimmed"
}

append_allowed_web_origin() {
  local origin
  origin="$(normalize_origin "${1:-}")"
  if [[ -z "$origin" ]]; then
    return
  fi

  local current="${AIBOT_SERVER_ALLOWED_WEB_ORIGINS:-}"
  if [[ -n "$current" ]]; then
    local existing
    local -a entries=()
    IFS=',' read -r -a entries <<< "$current"
    for existing in "${entries[@]}"; do
      if [[ "$(normalize_origin "$existing")" == "$origin" ]]; then
        return
      fi
    done
  fi

  if [[ -z "$current" ]]; then
    AIBOT_SERVER_ALLOWED_WEB_ORIGINS="$origin"
    return
  fi
  AIBOT_SERVER_ALLOWED_WEB_ORIGINS="${current},${origin}"
}

configure_allowed_web_origins_for_web_debug() {
  if [[ -z "$WEB_PORT" ]]; then
    return
  fi
  if ! [[ "$WEB_PORT" =~ ^[0-9]+$ ]]; then
    echo "[dev-up] WARN invalid WEB_PORT=${WEB_PORT}, skip allowed web origin injection"
    return
  fi

  append_allowed_web_origin "http://127.0.0.1:${WEB_PORT}"
  append_allowed_web_origin "http://localhost:${WEB_PORT}"

  export AIBOT_SERVER_ALLOWED_WEB_ORIGINS
  echo "[dev-up] allowed web origins: ${AIBOT_SERVER_ALLOWED_WEB_ORIGINS}"
}

mkdir -p "${PID_DIR}" "${LOG_DIR}"
configure_allowed_web_origins_for_web_debug

API_PORT="${API_PORT}" WS_PORT="${WS_PORT}" LLM_PORT="${LLM_PORT}" PUSH_PORT="${PUSH_PORT}" GATEWAY_PORT="${GATEWAY_PORT}" \
  "${ROOT_DIR}/scripts/dev_stop.sh"

start_service() {
  local name="$1"
  local logfile="${LOG_DIR}/${name}.log"
  local pidfile="${PID_DIR}/${name}.pid"

  echo "[dev-up] starting ${name} -> ${logfile}"
  (
    cd "${ROOT_DIR}"
    exec nohup "${GO_BIN}" run "./cmd/${name}" "${CONFIG_PATH}" </dev/null >>"${logfile}" 2>&1
  ) &

  local pid=$!
  echo "${pid}" > "${pidfile}"

  sleep 0.3
  if ! kill -0 "${pid}" >/dev/null 2>&1; then
    echo "[dev-up] failed to start ${name}, recent log:"
    tail -n 80 "${logfile}" || true
    exit 1
  fi
  echo "[dev-up] ${name} pid=${pid}"
}

wait_port() {
  local port="$1"
  local name="$2"
  if ! command -v lsof >/dev/null 2>&1; then
    return 0
  fi
  for _ in $(seq 1 80); do
    if lsof -ti "tcp:${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "[dev-up] ${name} listening on :${port}"
      return 0
    fi
    sleep 0.25
  done
  echo "[dev-up] WARN ${name} not confirmed on :${port} yet"
}

start_service api
start_service ws
start_service llm
start_service push
start_service gateway

wait_port "${API_PORT}" api
wait_port "${WS_PORT}" ws
wait_port "${LLM_PORT}" llm
wait_port "${PUSH_PORT}" push
wait_port "${GATEWAY_PORT}" gateway

DEV_ADMIN_USER="${DEV_ADMIN_USER:-admin}"
DEV_ADMIN_PASS="${DEV_ADMIN_PASS:-Admin123A456}"
WEB_URL="http://127.0.0.1:${API_PORT}/"
ADMIN_URL="http://127.0.0.1:${API_PORT}/admin"
WEB_DIST_INDEX="${ROOT_DIR}/internal/webapp/dist/index.html"

echo "[dev-up] bootstrapping default admin account..."
(
  cd "${ROOT_DIR}"
  "${GO_BIN}" run ./cmd/admin_bootstrap \
    -config "${CONFIG_PATH}" \
    -username "${DEV_ADMIN_USER}" \
    -password "${DEV_ADMIN_PASS}" \
    -nickname "Dev Admin"
) 2>&1 | sed 's/^/[dev-up] /'

# --- voicebridge (Python, optional) ---
VOICEBRIDGE_DIR="${ROOT_DIR}/../voicebridge"
if [ "${SKIP_VOICEBRIDGE:-0}" = "1" ]; then
  echo "[dev-up] skip voicebridge (SKIP_VOICEBRIDGE=1)"
elif [ ! -d "${VOICEBRIDGE_DIR}" ]; then
  echo "[dev-up] skip voicebridge (directory not found)"
elif [ ! -f "${VOICEBRIDGE_DIR}/.env" ]; then
  echo "[dev-up] skip voicebridge (.env missing — copy voicebridge/.env.example and fill in keys)"
else
  PYTHON_BIN="${PYTHON_BIN:-python3}"
  if ! command -v "${PYTHON_BIN}" >/dev/null 2>&1; then
    echo "[dev-up] skip voicebridge (python3 not found)"
  else
    # 优先使用 venv
    if [ -f "${VOICEBRIDGE_DIR}/.venv/bin/python" ]; then
      PYTHON_BIN="${VOICEBRIDGE_DIR}/.venv/bin/python"
    fi
    VBRIDGE_LOG="${LOG_DIR}/voicebridge.log"
    echo "[dev-up] starting voicebridge -> ${VBRIDGE_LOG}"
    (cd "${VOICEBRIDGE_DIR}" && exec nohup "${PYTHON_BIN}" main.py </dev/null >>"${VBRIDGE_LOG}" 2>&1) &
    VBRIDGE_PID=$!
    echo "${VBRIDGE_PID}" > "${PID_DIR}/voicebridge.pid"
    sleep 0.5
    if kill -0 "${VBRIDGE_PID}" >/dev/null 2>&1; then
      echo "[dev-up] voicebridge pid=${VBRIDGE_PID}"
    else
      echo "[dev-up] WARN voicebridge failed to start, check ${VBRIDGE_LOG}"
    fi
  fi
fi

echo "[dev-up] done"
echo "[dev-up] pids: ${PID_DIR}"
echo "[dev-up] logs: ${LOG_DIR}"
echo ""
echo "=========================================="
echo "  Web App:     ${WEB_URL}"
echo "  Admin Panel: ${ADMIN_URL}"
echo "  Username:    ${DEV_ADMIN_USER}"
echo "  Password:    ${DEV_ADMIN_PASS}"
echo "  Gateway:     http://127.0.0.1:${GATEWAY_PORT}/anthropic/v1/messages (Claude 中转测试用)"
if [ ! -f "${WEB_DIST_INDEX}" ]; then
  echo "  Web Bundle:  missing (${WEB_DIST_INDEX})"
  echo "               run ./scripts/sync_web_dist.sh if you need the embedded web bundle"
fi
echo "=========================================="
