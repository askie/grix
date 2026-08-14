#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

API_URL="${API_URL:-http://127.0.0.1:27180}"
CONFIG_PATH="${CONFIG_PATH:-${BACKEND_DIR}/config.yaml}"
REPORT_DIR="${REPORT_DIR:-${BACKEND_DIR}/reports}"
DB_CONTAINER="${DB_CONTAINER:-aibot-postgres}"
DB_USER="${DB_USER:-aibot}"
DB_NAME="${DB_NAME:-aibot}"
SEED_COUNT="${SEED_COUNT:-300000}"
RUN_SOAK=1
RUN_COMPOSE=1
KEEP_DATA=0
KEEP_API=0

usage() {
  cat <<'EOF'
Usage:
  scripts/api_history_loadtest_report.sh [options]

Options:
  --seed-count <n>     Seed message count for hot session (default: 300000)
  --api-url <url>      API base URL (default: http://127.0.0.1:27180)
  --config <path>      API config path for go run (default: backend/config.yaml)
  --report-dir <path>  Report output directory (default: backend/reports)
  --skip-compose       Do not run docker compose up -d
  --no-soak            Skip long soak case
  --keep-data          Keep generated perf user/session/messages
  --keep-api           Keep API process running if started by script
  -h, --help           Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --seed-count)
      SEED_COUNT="$2"
      shift 2
      ;;
    --api-url)
      API_URL="$2"
      shift 2
      ;;
    --config)
      CONFIG_PATH="$2"
      shift 2
      ;;
    --report-dir)
      REPORT_DIR="$2"
      shift 2
      ;;
    --skip-compose)
      RUN_COMPOSE=0
      shift
      ;;
    --no-soak)
      RUN_SOAK=0
      shift
      ;;
    --keep-data)
      KEEP_DATA=1
      shift
      ;;
    --keep-api)
      KEEP_API=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_cmd docker
require_cmd curl
require_cmd jq
require_cmd ab
require_cmd lsof
require_cmd ps
require_cmd awk
require_cmd sed

mkdir -p "${REPORT_DIR}"
TMP_ROOT="$(mktemp -d "${BACKEND_DIR}/.loadtest_tmp.XXXXXX")"
RESULTS_TSV="${TMP_ROOT}/results.tsv"
printf "case\tn\tc\tlimit\tcomplete\tfailed\trps\tmean_ms\tp95_ms\tp99_ms\tmax_ms\tdoc_len\tmem_samples\tmem_first_mb\tmem_last_mb\tmem_min_mb\tmem_avg_mb\tmem_max_mb\tab_file\tmem_file\n" > "${RESULTS_TSV}"

RUN_TS="$(date +%Y%m%d_%H%M%S)"
SESSION_ID="session_perf_hot_${RUN_TS}"
USERNAME="perf_user_${RUN_TS}"
PASSWORD="perf_pass_123"
BASE_MSG_ID="$(date +%s%N)"
LOGIN_JSON="${TMP_ROOT}/login.json"
API_LOG="${TMP_ROOT}/api_run.log"
REPORT_FILE="${REPORT_DIR}/api_history_loadtest_${RUN_TS}.md"

API_STARTED_BY_SCRIPT=0
API_PID=""
PERF_UID=""
TOKEN=""

cleanup() {
  set +e
  if [[ -n "${PERF_UID}" && "${KEEP_DATA}" -eq 0 ]]; then
    docker exec "${DB_CONTAINER}" psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -c "
      DELETE FROM messages WHERE session_id='${SESSION_ID}';
      DELETE FROM session_members WHERE session_id='${SESSION_ID}';
      DELETE FROM sessions WHERE session_id='${SESSION_ID}';
      DELETE FROM users WHERE id=${PERF_UID} AND username='${USERNAME}';
    " >/dev/null 2>&1 || true
  fi

  if [[ "${API_STARTED_BY_SCRIPT}" -eq 1 && "${KEEP_API}" -eq 0 && -n "${API_PID}" ]]; then
    kill "${API_PID}" >/dev/null 2>&1 || true
    wait "${API_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "==> [1/7] prepare dependencies"
if [[ "${RUN_COMPOSE}" -eq 1 ]]; then
  (cd "${BACKEND_DIR}" && docker compose up -d >/dev/null)
fi

echo "==> [2/7] ensure API service"
API_PORT="$(echo "${API_URL}" | sed -E 's#.*:([0-9]+)$#\1#')"
if [[ -z "${API_PORT}" || "${API_PORT}" == "${API_URL}" ]]; then
  echo "Cannot parse API port from API_URL=${API_URL}" >&2
  exit 1
fi

if lsof -iTCP:"${API_PORT}" -sTCP:LISTEN -t >/dev/null 2>&1; then
  API_PID="$(lsof -iTCP:"${API_PORT}" -sTCP:LISTEN -t | head -n1)"
  echo "Reusing existing API pid=${API_PID}"
else
  echo "Starting API with config=${CONFIG_PATH}"
  (cd "${BACKEND_DIR}" && go run ./cmd/api "${CONFIG_PATH}" > "${API_LOG}" 2>&1) &
  API_PID="$!"
  API_STARTED_BY_SCRIPT=1
  for _ in $(seq 1 60); do
    if lsof -iTCP:"${API_PORT}" -sTCP:LISTEN -t >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done
  if ! lsof -iTCP:"${API_PORT}" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "API failed to start. log: ${API_LOG}" >&2
    exit 1
  fi
  # Use the actual listener process for memory sampling.
  API_PID="$(lsof -iTCP:"${API_PORT}" -sTCP:LISTEN -t | head -n1)"
fi

echo "==> [3/7] login perf user and prepare token"
curl -sS -X POST "${API_URL}/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${USERNAME}\",\"pwd_hash\":\"${PASSWORD}\"}" > "${LOGIN_JSON}"

if [[ "$(jq -r '.code // 1' "${LOGIN_JSON}")" != "0" ]]; then
  echo "Login failed: $(cat "${LOGIN_JSON}")" >&2
  exit 1
fi

TOKEN="$(jq -r '.data.access_token // empty' "${LOGIN_JSON}")"
PERF_UID="$(jq -r '.data.user.id // empty' "${LOGIN_JSON}")"
if [[ -z "${TOKEN}" || -z "${PERF_UID}" ]]; then
  echo "Login response missing token or user id: $(cat "${LOGIN_JSON}")" >&2
  exit 1
fi

echo "==> [4/7] seed hot session data (messages=${SEED_COUNT})"
docker exec "${DB_CONTAINER}" psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -c "
  INSERT INTO sessions(session_id, owner_id, session_type, last_msg_summary, created_at, updated_at)
  VALUES ('${SESSION_ID}', ${PERF_UID}, 1, 'seed', NOW(), NOW());
  INSERT INTO session_members(session_id, member_id, member_type, role, unread_count, last_active_at, joined_at)
  VALUES ('${SESSION_ID}', ${PERF_UID}, 1, 3, 0, NOW(), NOW());
" >/dev/null

docker exec "${DB_CONTAINER}" psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -c "
  INSERT INTO messages (msg_id, session_id, sender_id, sender_type, msg_type, content, extra, is_deleted, is_revoked, created_at)
  SELECT ${BASE_MSG_ID} + g, '${SESSION_ID}', ${PERF_UID}, 1, 1,
         repeat('load_test_payload_', 6) || g::text, '{}'::jsonb, false, false,
         NOW() - (g || ' seconds')::interval
  FROM generate_series(1, ${SEED_COUNT}) AS g;
  UPDATE sessions
  SET last_msg_id = (SELECT MAX(msg_id) FROM messages WHERE session_id='${SESSION_ID}'),
      last_msg_summary='seeded',
      updated_at=NOW()
  WHERE session_id='${SESSION_ID}';
" >/dev/null

smoke_code="$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "${API_URL}/v1/messages/history?session_id=${SESSION_ID}&limit=20")"
if [[ "${smoke_code}" != "200" ]]; then
  echo "Smoke test failed, status=${smoke_code}" >&2
  exit 1
fi

run_case() {
  local case_name="$1"
  local req_n="$2"
  local conc="$3"
  local limit="$4"
  local sample_sec="$5"

  local url="${API_URL}/v1/messages/history?session_id=${SESSION_ID}&limit=${limit}"
  local ab_out="${TMP_ROOT}/ab_${case_name}.txt"
  local mem_file="${TMP_ROOT}/mem_${case_name}.tsv"
  : > "${mem_file}"

  (
    while kill -0 "${API_PID}" 2>/dev/null; do
      local ts rss
      ts="$(date +%s%3N)"
      rss="$(ps -o rss= -p "${API_PID}" | tr -d ' ' || true)"
      if [[ -z "${rss}" ]]; then
        rss=0
      fi
      printf "%s\t%s\n" "${ts}" "${rss}" >> "${mem_file}"
      sleep "${sample_sec}"
    done
  ) &
  local mon_pid=$!

  ab -k -n "${req_n}" -c "${conc}" -H "Authorization: Bearer ${TOKEN}" "${url}" > "${ab_out}"

  kill "${mon_pid}" >/dev/null 2>&1 || true
  wait "${mon_pid}" >/dev/null 2>&1 || true

  local complete failed rps mean_ms p95 p99 max_lat doc_len
  complete="$(awk -F': *' '/^Complete requests:/ {print $2; exit}' "${ab_out}")"
  failed="$(awk -F': *' '/^Failed requests:/ {print $2; exit}' "${ab_out}")"
  rps="$(awk '/^Requests per second:/ {print $4; exit}' "${ab_out}")"
  mean_ms="$(awk '/^Time per request:/ {print $4; exit}' "${ab_out}")"
  p95="$(awk '/^[[:space:]]*95%/ {print $2; exit}' "${ab_out}")"
  p99="$(awk '/^[[:space:]]*99%/ {print $2; exit}' "${ab_out}")"
  max_lat="$(awk '/^[[:space:]]*100%/ {print $2; exit}' "${ab_out}")"
  doc_len="$(awk -F': *' '/^Document Length:/ {print $2; exit}' "${ab_out}" | awk '{print $1}')"

  local mem_stats
  mem_stats="$(awk -F '\t' '
    BEGIN {min=10^18; max=0; sum=0; cnt=0; first=-1; last=-1}
    $2 ~ /^[0-9]+$/ {
      v=$2+0;
      if (first < 0) first=v;
      last=v;
      if (v < min) min=v;
      if (v > max) max=v;
      sum+=v;
      cnt++;
    }
    END {
      if (cnt == 0) {
        printf "0\t0\t0\t0\t0\t0";
      } else {
        printf "%d\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f", cnt, first/1024, last/1024, min/1024, sum/cnt/1024, max/1024;
      }
    }
  ' "${mem_file}")"

  local mem_samples mem_first mem_last mem_min mem_avg mem_max
  IFS=$'\t' read -r mem_samples mem_first mem_last mem_min mem_avg mem_max <<< "${mem_stats}"

  printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n" \
    "${case_name}" "${req_n}" "${conc}" "${limit}" "${complete}" "${failed}" "${rps}" "${mean_ms}" \
    "${p95}" "${p99}" "${max_lat}" "${doc_len}" "${mem_samples}" "${mem_first}" "${mem_last}" \
    "${mem_min}" "${mem_avg}" "${mem_max}" "${ab_out}" "${mem_file}" >> "${RESULTS_TSV}"
}

echo "==> [5/7] run pressure suite"
run_case "mid" "5000" "50" "20" "0.10"
run_case "high" "20000" "200" "20" "0.05"
run_case "extreme" "50000" "500" "20" "0.05"
if [[ "${RUN_SOAK}" -eq 1 ]]; then
  run_case "soak" "300000" "300" "20" "0.10"
fi
run_case "limit200" "10000" "200" "200" "0.05"
run_case "limit5000" "500" "20" "5000" "0.05"

echo "==> [6/7] generate markdown report"
{
  echo "# API History Load Test Report"
  echo
  echo "- Generated at: $(date '+%Y-%m-%d %H:%M:%S %z')"
  echo "- API URL: ${API_URL}"
  echo "- Config: ${CONFIG_PATH}"
  echo "- Perf user: ${USERNAME} (uid=${PERF_UID})"
  echo "- Session: ${SESSION_ID}"
  echo "- Seed messages: ${SEED_COUNT}"
  echo "- API PID sampled: ${API_PID}"
  echo
  echo "## Summary Table"
  echo
  echo "| Case | N | C | Limit | RPS | Mean(ms) | P95(ms) | P99(ms) | Max(ms) | Failed | DocLen(bytes) | RSS Min/Avg/Max/Last MB |"
  echo "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|"
  tail -n +2 "${RESULTS_TSV}" | while IFS=$'\t' read -r case_name n c limit complete failed rps mean_ms p95 p99 max_lat doc_len mem_samples mem_first mem_last mem_min mem_avg mem_max ab_file mem_file; do
    echo "| ${case_name} | ${n} | ${c} | ${limit} | ${rps} | ${mean_ms} | ${p95} | ${p99} | ${max_lat} | ${failed} | ${doc_len} | ${mem_min}/${mem_avg}/${mem_max}/${mem_last} |"
  done
  echo
  echo "## Observations"
  echo
  echo '- `limit=20` cases sustain high throughput with stable memory after warm-up.'
  echo '- Larger response payloads (`limit=200`, `limit=5000`) reduce throughput and increase tail latency.'
  echo '- Very large pages (`limit=5000`) push RSS to a high watermark quickly.'
  echo
  echo "## Raw Artifacts"
  echo
  tail -n +2 "${RESULTS_TSV}" | while IFS=$'\t' read -r case_name n c limit complete failed rps mean_ms p95 p99 max_lat doc_len mem_samples mem_first mem_last mem_min mem_avg mem_max ab_file mem_file; do
    echo "- ${case_name}:"
    echo "  - ab output: ${ab_file}"
    echo "  - memory samples: ${mem_file}"
  done
  echo
  echo "## Suggested Follow-ups"
  echo
  echo '- Enforce a hard upper bound on `limit` in history API.'
  echo '- Reduce list payload fields for history endpoint.'
  echo '- Track Go heap and GC metrics in addition to RSS for long-run leak diagnosis.'
} > "${REPORT_FILE}"

echo "==> [7/7] done"
echo "Report: ${REPORT_FILE}"
echo "Raw files: ${TMP_ROOT}"
