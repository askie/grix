#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"
IPA_PATH="${1:-${FRONTEND_ROOT}/build/ios/ipa/Grix.ipa}"

fail() {
  echo "[ios-upload] ERROR: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

require_env() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "missing required env: ${name}"
}

main() {
  local output status

  require_unified_release_call "ios-upload" "./scripts/release.sh mobile-ios-ipa"

  require_cmd xcrun
  require_env APPSTORE_API_KEY_ID
  require_env APPSTORE_API_ISSUER_ID

  [[ -f "${IPA_PATH}" ]] || fail "ipa file not found: ${IPA_PATH}"

  if [[ -n "${APPSTORE_API_PRIVATE_KEYS_DIR:-}" ]]; then
    export API_PRIVATE_KEYS_DIR="${APPSTORE_API_PRIVATE_KEYS_DIR}"
  fi

  local altool_timeout="${IOS_ALTOOL_UPLOAD_TIMEOUT_SEC:-120}"

  echo "[ios-upload] uploading IPA: ${IPA_PATH} (timeout=${altool_timeout}s)"
  set +e
  output="$(timeout "${altool_timeout}" xcrun altool --upload-app \
    -f "${IPA_PATH}" \
    --apiKey "${APPSTORE_API_KEY_ID}" \
    --apiIssuer "${APPSTORE_API_ISSUER_ID}" \
    --show-progress \
    --output-format json 2>&1)"
  status=$?
  set -e

  printf '%s\n' "${output}"

  # timeout(1) exits 124 when the command times out
  if [[ "${status}" -eq 124 ]]; then
    echo "[ios-upload] WARNING: altool timed out after ${altool_timeout}s"
    echo "[ios-upload] upload may have completed; the compliance/distribute scripts will verify"
    echo "[ios-upload] proceeding without error to avoid wasting build numbers on retry"
    exit 0
  fi

  if [[ "${status}" -ne 0 ]]; then
    fail "altool upload command failed with exit code ${status}"
  fi

  if printf '%s\n' "${output}" | grep -q '"product-errors"'; then
    fail "altool reported product-errors during upload"
  fi

  if printf '%s\n' "${output}" | grep -q 'ERROR:'; then
    fail "altool reported upload errors"
  fi
}

main "$@"
