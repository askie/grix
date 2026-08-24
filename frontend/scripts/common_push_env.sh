#!/usr/bin/env bash
set -euo pipefail

FRONTEND_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_ROOT="$(cd "${FRONTEND_ROOT}/.." && pwd)"
GRIX_OPS_DIR="${GRIX_OPS_DIR:-$(cd "${SOURCE_ROOT}/.." && pwd)/grix-ops}"
COMMON_RELEASE_ENV="${GRIX_OPS_DIR}/scripts/common_release_env.sh"
if [[ ! -f "${COMMON_RELEASE_ENV}" ]]; then
  echo "[push-env] missing private release environment: ${COMMON_RELEASE_ENV}" >&2
  echo "[push-env] set GRIX_OPS_DIR to the private grix-ops checkout." >&2
  exit 1
fi
# shellcheck source=/dev/null
. "${COMMON_RELEASE_ENV}"
DEFAULT_IOS_BUNDLE_ID="pub.dhf.grix"

require_unified_release_call() {
  local tag="$1"
  local hint="$2"
  local parent_cmd

  if [ "${GRIX_RELEASE_UNIFIED_CALL:-0}" = "1" ] && \
     [ "${GRIX_RELEASE_ENTRYPOINT:-}" = "frontend" ]; then
    return 0
  fi

  if [ "${AIBOT_RELEASE_UNIFIED_CALL:-0}" != "1" ]; then
    echo "[${tag}] ERROR: direct invocation is blocked; use ${hint}" >&2
    exit 1
  fi

  # Backward compatibility for the retired monolithic grix-ops entrypoint.
  parent_cmd="$(ps -o command= -p "${PPID}" 2>/dev/null || true)"
  if [[ "${parent_cmd}" != *"scripts/release.sh"* ]]; then
    echo "[${tag}] ERROR: direct invocation is blocked; use ${hint}" >&2
    exit 1
  fi
}

default_push_env_file() {
  release_resolve_push_env_file "${FRONTEND_ROOT}"
}

load_push_env() {
  local env_file="${PUSH_ENV_FILE:-$(default_push_env_file)}"
  if [ ! -f "${env_file}" ]; then
    echo "[push-env] missing env file: ${env_file}" >&2
    echo "[push-env] copy ${FRONTEND_ROOT}/scripts/push.env.example to ${env_file} and fill it." >&2
    exit 1
  fi

  echo "[push-env] loading ${env_file}"
  set -a
  # shellcheck source=/dev/null
  . "${env_file}"
  set +a
}

require_env() {
  local name="$1"
  if [ -z "${!name:-}" ]; then
    echo "[push-env] required env missing: ${name}" >&2
    exit 1
  fi
}

require_frontend_base_env() {
  require_env API_BASE_URL
  require_env WS_URL
}

require_url_scheme() {
  local name="$1"
  local scheme="$2"
  local value="${!name:-}"
  if [[ "${value}" != "${scheme}://"* ]]; then
    echo "[push-env] ${name} must start with ${scheme}://, got: ${value}" >&2
    exit 1
  fi
}

require_url_suffix() {
  local name="$1"
  local suffix="$2"
  local value="${!name:-}"
  if [[ "${value}" != *"${suffix}" ]]; then
    echo "[push-env] ${name} must end with ${suffix}, got: ${value}" >&2
    exit 1
  fi
}

extract_url_host() {
  local url="$1"
  local without_scheme="${url#*://}"
  local authority="${without_scheme%%/*}"
  local host
  if [[ "${authority}" == \[*\]* ]]; then
    host="${authority%%]*}"
    host="${host#[}"
  else
    host="${authority%%:*}"
  fi
  echo "${host}"
}

is_private_or_local_host() {
  local host="$1"
  case "${host}" in
    "" | localhost | 0.0.0.0 | 127.* | ::1)
      return 0
      ;;
  esac

  if [[ "${host}" =~ ^10\. ]]; then
    return 0
  fi
  if [[ "${host}" =~ ^192\.168\. ]]; then
    return 0
  fi
  if [[ "${host}" =~ ^172\.(1[6-9]|2[0-9]|3[0-1])\. ]]; then
    return 0
  fi
  if [[ "${host}" =~ ^169\.254\. ]]; then
    return 0
  fi
  return 1
}

require_public_host_url() {
  local name="$1"
  local value="${!name:-}"
  local host
  host="$(extract_url_host "${value}")"
  if is_private_or_local_host "${host}"; then
    echo "[push-env] ${name} must use a public host for release packaging, got: ${value}" >&2
    exit 1
  fi
}

require_ios_release_endpoint_env() {
  require_frontend_base_env
  require_url_scheme API_BASE_URL https
  require_url_scheme WS_URL wss
  require_url_suffix API_BASE_URL /v1
  require_url_suffix WS_URL /ws
  require_public_host_url API_BASE_URL
  require_public_host_url WS_URL
}

require_ios_release_apns_env() {
  local expected_topic="${IOS_BUNDLE_ID:-${DEFAULT_IOS_BUNDLE_ID}}"
  require_env AIBOT_PUSH_APNS_TOPIC
  require_env AIBOT_PUSH_APNS_IS_PRODUCTION

  if [ "${AIBOT_PUSH_APNS_IS_PRODUCTION}" != "true" ]; then
    echo "[push-env] AIBOT_PUSH_APNS_IS_PRODUCTION must be true for iOS release packaging" >&2
    exit 1
  fi
  if [ "${AIBOT_PUSH_APNS_TOPIC}" != "${expected_topic}" ]; then
    echo "[push-env] AIBOT_PUSH_APNS_TOPIC must equal ${expected_topic}, got: ${AIBOT_PUSH_APNS_TOPIC}" >&2
    exit 1
  fi
}

require_ios_release_env() {
  require_ios_release_endpoint_env
  require_ios_release_apns_env
}

require_ios_distribution_identity() {
  local team_id="${IOS_TEAM_ID:-}"
  if ! command -v security >/dev/null 2>&1; then
    echo "[push-env] missing required command: security" >&2
    exit 1
  fi

  local identities
  identities="$(security find-identity -v -p codesigning 2>/dev/null || true)"

  if [ -z "${identities}" ]; then
    echo "[push-env] no code signing identities found in keychain" >&2
    exit 1
  fi

  local pattern='Apple Distribution|iOS Distribution'
  if [ -n "${team_id}" ]; then
    if ! printf '%s\n' "${identities}" | grep -E "${pattern}" | grep -q "${team_id}"; then
      echo "[push-env] missing Apple Distribution identity for team ${team_id}" >&2
      echo "[push-env] fix: Xcode > Settings > Accounts > Manage Certificates... > add Apple Distribution" >&2
      exit 1
    fi
    return 0
  fi

  if ! printf '%s\n' "${identities}" | grep -Eq "${pattern}"; then
    echo "[push-env] missing Apple Distribution/iOS Distribution identity in keychain" >&2
    echo "[push-env] fix: Xcode > Settings > Accounts > Manage Certificates... > add Apple Distribution" >&2
    exit 1
  fi
}

require_ios_release_signing_assets() {
  require_ios_distribution_identity
}

build_flutter_release_args() {
  FLUTTER_RELEASE_ARGS=()
  if [ "${BUILD_MODE:-release}" = "release" ]; then
    local debug_info_dir="${FRONTEND_ROOT}/build/debug-info"
    mkdir -p "${debug_info_dir}"
    FLUTTER_RELEASE_ARGS+=(
      "--split-debug-info=${debug_info_dir}"
      "--obfuscate"
    )
  fi
}

build_flutter_define_args() {
  # 区域端点显式钉死：中国大陆复用主后端，全球默认 gb.grix.im（AWS 全球区），可经 env 覆盖
  local global_api_url="${GLOBAL_API_URL:-https://gb.grix.im/v1}"
  local global_ws_url="${GLOBAL_WS_URL:-wss://ws.grix.im/ws}"

  # 区域切换是命脉功能，地址错了整个区域不可用。全球区与中国区一视同仁：
  # 先做格式守卫（https/wss、/v1、/ws、公网域名），再做探活守卫（确认背后是真后端）。
  GLOBAL_API_URL="${global_api_url}" require_url_scheme GLOBAL_API_URL https
  GLOBAL_API_URL="${global_api_url}" require_url_suffix GLOBAL_API_URL /v1
  GLOBAL_API_URL="${global_api_url}" require_public_host_url GLOBAL_API_URL
  GLOBAL_WS_URL="${global_ws_url}" require_url_scheme GLOBAL_WS_URL wss
  GLOBAL_WS_URL="${global_ws_url}" require_url_suffix GLOBAL_WS_URL /ws
  GLOBAL_WS_URL="${global_ws_url}" require_public_host_url GLOBAL_WS_URL
  release_require_live_api_backend "中国区(CN_API_URL)" "${API_BASE_URL}"
  release_require_live_api_backend "全球区(GLOBAL_API_URL)" "${global_api_url}"

  FLUTTER_DEFINE_ARGS=(
    "--dart-define=API_BASE_URL=${API_BASE_URL}"
    "--dart-define=WS_URL=${WS_URL}"
    "--dart-define=CN_API_URL=${API_BASE_URL}"
    "--dart-define=CN_WS_URL=${WS_URL}"
    "--dart-define=GLOBAL_API_URL=${global_api_url}"
    "--dart-define=GLOBAL_WS_URL=${global_ws_url}"
  )
  if [ -n "${WEB_PUSH_VAPID_PUBLIC_KEY:-}" ]; then
    FLUTTER_DEFINE_ARGS+=("--dart-define=WEB_PUSH_VAPID_PUBLIC_KEY=${WEB_PUSH_VAPID_PUBLIC_KEY}")
  fi
  if [ -n "${SENTRY_DSN:-}" ]; then
    FLUTTER_DEFINE_ARGS+=("--dart-define=SENTRY_DSN=${SENTRY_DSN}")
  fi
}
