#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BENCH_FILE="tool/benchmarks/chat_page_perf_benchmark.dart"

COUNTS=${CHAT_BENCH_COUNTS:-"1000 3000 6000"}
ITERATIONS=${CHAT_BENCH_ITERATIONS:-5}
MESSAGE_LENGTH=${CHAT_BENCH_MESSAGE_LENGTH:-280}
MARKDOWN_RATIO=${CHAT_BENCH_MARKDOWN_RATIO:-0.45}
PARSE_SAMPLE_COUNT=${CHAT_BENCH_PARSE_SAMPLE_COUNT:-2000}
LOAD_MODE=${CHAT_BENCH_LOAD_MODE:-prefilled_full}
TRIM_PROBE_APPEND=${CHAT_BENCH_TRIM_PROBE_APPEND:-0}
CHAT_TYPE=${CHAT_BENCH_CHAT_TYPE:-private}
SENDER_POOL_SIZE=${CHAT_BENCH_SENDER_POOL_SIZE:-2}
MENTION_ITEMS=${CHAT_BENCH_MENTION_ITEMS:-0}

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/chat-bench.XXXXXX")"
trap 'rm -rf "${TMP_DIR}"' EXIT

cd "${FRONTEND_DIR}"

echo "Running chat performance benchmark"
echo "  counts: ${COUNTS}"
echo "  iterations: ${ITERATIONS}"
echo "  message_length: ${MESSAGE_LENGTH}"
echo "  markdown_ratio: ${MARKDOWN_RATIO}"
echo "  parse_sample_count: ${PARSE_SAMPLE_COUNT}"
echo "  load_mode: ${LOAD_MODE}"
echo "  trim_probe_append: ${TRIM_PROBE_APPEND}"
echo "  chat_type: ${CHAT_TYPE}"
echo "  sender_pool_size: ${SENDER_POOL_SIZE}"
echo "  mention_items: ${MENTION_ITEMS}"

for count in ${COUNTS}; do
  log_file="${TMP_DIR}/chat_bench_${count}.log"
  echo
  echo "=== Benchmark message_count=${count} ==="

  flutter test "${BENCH_FILE}" \
    --dart-define=CHAT_BENCH_ENABLE=true \
    --dart-define=CHAT_BENCH_MESSAGE_COUNT="${count}" \
    --dart-define=CHAT_BENCH_ITERATIONS="${ITERATIONS}" \
    --dart-define=CHAT_BENCH_MESSAGE_LENGTH="${MESSAGE_LENGTH}" \
    --dart-define=CHAT_BENCH_MARKDOWN_RATIO="${MARKDOWN_RATIO}" \
    --dart-define=CHAT_BENCH_PARSE_SAMPLE_COUNT="${PARSE_SAMPLE_COUNT}" \
    --dart-define=CHAT_BENCH_LOAD_MODE="${LOAD_MODE}" \
    --dart-define=CHAT_BENCH_TRIM_PROBE_APPEND="${TRIM_PROBE_APPEND}" \
    --dart-define=CHAT_BENCH_CHAT_TYPE="${CHAT_TYPE}" \
    --dart-define=CHAT_BENCH_SENDER_POOL_SIZE="${SENDER_POOL_SIZE}" \
    --dart-define=CHAT_BENCH_MENTION_ITEMS="${MENTION_ITEMS}" \
    --reporter=expanded | tee "${log_file}"

  echo "--- Summary (message_count=${count}) ---"
  grep 'BENCH_CHAT_SUMMARY' "${log_file}" || {
    echo "BENCH_CHAT_SUMMARY not found" >&2
    exit 1
  }
  grep 'BENCH_PARSE_SUMMARY' "${log_file}" || {
    echo "BENCH_PARSE_SUMMARY not found" >&2
    exit 1
  }
done

echo
echo "All benchmark runs completed."
