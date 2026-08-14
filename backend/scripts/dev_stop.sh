#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="${ROOT_DIR}/.run"
PID_DIR="${RUN_DIR}/pids"

API_PORT="${API_PORT:-27180}"
WS_PORT="${WS_PORT:-27189}"
LLM_PORT="${LLM_PORT:-27182}"
PUSH_PORT="${PUSH_PORT:-27183}"
GATEWAY_PORT="${GATEWAY_PORT:-27184}"

stop_pid() {
  local name="$1"
  local pid="$2"
  if ! kill -0 "${pid}" >/dev/null 2>&1; then
    return 0
  fi

  echo "[dev-stop] stopping ${name} pid=${pid}"
  kill -TERM "${pid}" >/dev/null 2>&1 || true
  for _ in $(seq 1 50); do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done

  echo "[dev-stop] force kill ${name} pid=${pid}"
  kill -KILL "${pid}" >/dev/null 2>&1 || true
}

stop_from_pid_file() {
  local name="$1"
  local pidfile="${PID_DIR}/${name}.pid"
  if [[ ! -f "${pidfile}" ]]; then
    return 0
  fi

  local pid
  pid="$(cat "${pidfile}" 2>/dev/null || true)"
  if [[ -n "${pid}" ]]; then
    stop_pid "${name}" "${pid}"
  fi
  rm -f "${pidfile}"
}

kill_listen_port() {
  local port="$1"
  if ! command -v lsof >/dev/null 2>&1; then
    return 0
  fi

  local pids
  pids="$(lsof -ti "tcp:${port}" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -z "${pids}" ]]; then
    return 0
  fi

  echo "[dev-stop] killing listeners on tcp:${port}: ${pids}"
  # shellcheck disable=SC2086
  kill -TERM ${pids} >/dev/null 2>&1 || true
  sleep 0.4

  local alive=()
  local p
  for p in ${pids}; do
    if kill -0 "${p}" >/dev/null 2>&1; then
      alive+=("${p}")
    fi
  done
  if [[ ${#alive[@]} -gt 0 ]]; then
    # shellcheck disable=SC2068
    kill -KILL ${alive[@]} >/dev/null 2>&1 || true
  fi
}

mkdir -p "${PID_DIR}"

stop_from_pid_file api
stop_from_pid_file ws
stop_from_pid_file llm
stop_from_pid_file push
stop_from_pid_file gateway
stop_from_pid_file voicebridge

kill_listen_port "${API_PORT}"
kill_listen_port "${WS_PORT}"
kill_listen_port "${LLM_PORT}"
kill_listen_port "${PUSH_PORT}"
kill_listen_port "${GATEWAY_PORT}"

echo "[dev-stop] done"
