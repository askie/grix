#!/usr/bin/env bash
set -euo pipefail

# admin iOS IPA 上传脚本。
# 这次发布验证通过的路径是 Apple ID + app-specific password + provider public id；
# ASC API key 在当前账号/协议状态下可能会 403，因此 admin 上传默认不走 API key。

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ADMIN_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DEFAULT_PROVIDER_PUBLIC_ID="69a6de88-f2c3-47e3-e053-5b8c7c11a4d1"

fail() {
  echo "[admin-ios-upload] ERROR: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

require_env() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "missing required env: ${name}"
}

resolve_ipa_path() {
  local explicit_path="${1:-${IOS_IPA_PATH:-}}"
  if [[ -n "${explicit_path}" ]]; then
    printf '%s\n' "${explicit_path}"
    return
  fi

  find "${ADMIN_ROOT}/build/ios/ipa" -name '*.ipa' -print -quit 2>/dev/null || true
}

main() {
  local ipa_path provider_public_id output status

  if [[ "${AIBOT_RELEASE_UNIFIED_CALL:-0}" != "1" ]]; then
    fail "请通过 ./scripts/release.sh admin-ios-ipa 或 make ios-upload-testflight 调用"
  fi

  require_cmd xcrun
  require_env APPLE_ID
  require_env APPLE_APP_PASSWORD

  ipa_path="$(resolve_ipa_path "${1:-}")"
  [[ -n "${ipa_path}" && -f "${ipa_path}" ]] || fail "IPA file not found: ${ipa_path:-<empty>}"

  provider_public_id="${ADMIN_APPSTORE_PROVIDER_PUBLIC_ID:-${APPSTORE_PROVIDER_PUBLIC_ID:-${DEFAULT_PROVIDER_PUBLIC_ID}}}"

  echo "[admin-ios-upload] uploading IPA: ${ipa_path}"
  set +e
  output="$(xcrun altool --upload-app \
    -f "${ipa_path}" \
    --username "${APPLE_ID}" \
    --app-password "${APPLE_APP_PASSWORD}" \
    --provider-public-id "${provider_public_id}" \
    --show-progress \
    --output-format json 2>&1)"
  status=$?
  set -e

  printf '%s\n' "${output}"

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
