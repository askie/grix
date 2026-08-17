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

generate_jwt() {
  local output token

  if [[ -n "${APPSTORE_API_PRIVATE_KEYS_DIR:-}" ]]; then
    export API_PRIVATE_KEYS_DIR="${APPSTORE_API_PRIVATE_KEYS_DIR}"
  fi

  if output="$(xcrun altool --generate-jwt --apiKey "${APPSTORE_API_KEY_ID}" --apiIssuer "${APPSTORE_API_ISSUER_ID}" --output-format json 2>&1)"; then
    :
  else
    output="$(xcrun altool --generate-jwt --apiKey "${APPSTORE_API_KEY_ID}" --apiIssuer "${APPSTORE_API_ISSUER_ID}" 2>&1 || true)"
  fi

  token="$(printf '%s\n' "${output}" | jq -r '.. | .token? // empty' 2>/dev/null | head -n 1 || true)"
  if [[ -z "${token}" ]]; then
    token="$(printf '%s\n' "${output}" | grep -Eo '[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+' | head -n 1 || true)"
  fi

  [[ -n "${token}" ]] || fail "failed to generate App Store Connect JWT"
  printf '%s' "${token}"
}

# 从 IPA 读取 bundle id 与 build 号
read_ipa_metadata() {
  local tmp_plist

  tmp_plist="$(mktemp)"
  if ! unzip -p "${IPA_PATH}" "Payload/*.app/Info.plist" >"${tmp_plist}" 2>/dev/null; then
    rm -f "${tmp_plist}"
    fail "failed to read Info.plist from ipa: ${IPA_PATH}"
  fi

  IPA_BUNDLE_ID="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "${tmp_plist}" 2>/dev/null || true)"
  IPA_BUILD_NUMBER="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "${tmp_plist}" 2>/dev/null || true)"

  rm -f "${tmp_plist}"

  [[ -n "${IPA_BUNDLE_ID}" ]] || fail "cannot read CFBundleIdentifier from ipa"
  [[ -n "${IPA_BUILD_NUMBER}" ]] || fail "cannot read CFBundleVersion from ipa"
}

# 查询 ASC 中指定 build 是否已可见（存在即返回 0）
asc_build_visible() {
  local jwt response app_id

  read_ipa_metadata
  jwt="$(generate_jwt)"

  response="$(curl -sS --get "https://api.appstoreconnect.apple.com/v1/apps" \
    -H "Authorization: Bearer ${jwt}" \
    --data-urlencode "filter[bundleId]=${IPA_BUNDLE_ID}" \
    --data-urlencode "limit=1" 2>/dev/null || true)"
  app_id="$(printf '%s' "${response}" | jq -r '.data[0].id // empty' 2>/dev/null || true)"
  [[ -n "${app_id}" ]] || fail "app not found for bundle id: ${IPA_BUNDLE_ID}"

  response="$(curl -sS --get "https://api.appstoreconnect.apple.com/v1/builds" \
    -H "Authorization: Bearer ${jwt}" \
    --data-urlencode "filter[app]=${app_id}" \
    --data-urlencode "filter[version]=${IPA_BUILD_NUMBER}" \
    --data-urlencode "limit=1" 2>/dev/null || true)"

  if printf '%s' "${response}" | jq -e '.data[0].id // empty' >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

# 自动 resume 重传：altool 对同一 IPA 幂等，重跑会续传未到达的分片
retry_upload_on_timeout() {
  local attempt=1
  local max_attempts="${IOS_ALTOOL_UPLOAD_RETRY_ATTEMPTS:-3}"
  local retry_timeout="${IOS_ALTOOL_UPLOAD_RETRY_TIMEOUT_SEC:-600}"
  local output status

  while (( attempt <= max_attempts )); do
    echo "[ios-upload] WARNING: altool timed out, auto-retry (resume) ${attempt}/${max_attempts} (timeout=${retry_timeout}s)..."
    set +e
    output="$(timeout "${retry_timeout}" xcrun altool --upload-app \
      -f "${IPA_PATH}" \
      --apiKey "${APPSTORE_API_KEY_ID}" \
      --apiIssuer "${APPSTORE_API_ISSUER_ID}" \
      --show-progress \
      --output-format json 2>&1)"
    status=$?
    set -e

    printf '%s\n' "${output}"

    if [[ "${status}" -ne 124 ]]; then
      # 非超时退出：正常或失败
      if [[ "${status}" -eq 0 ]] && ! printf '%s\n' "${output}" | grep -q '"product-errors"'; then
        echo "[ios-upload] retry ${attempt} uploaded successfully"
        return 0
      fi
      return "${status}"
    fi
    attempt=$((attempt + 1))
  done

  echo "[ios-upload] ERROR: altool timed out after ${max_attempts} retries; checking ASC for partial upload..."
  if asc_build_visible; then
    echo "[ios-upload] build ${IPA_BUILD_NUMBER} already visible in ASC — upload completed despite timeout"
    return 0
  fi
  fail "altool upload timed out and build ${IPA_BUILD_NUMBER} is NOT visible in ASC; manual upload required: xcrun altool --upload-app -f ${IPA_PATH} --apiKey ... (no timeout)"
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

  local altool_timeout="${IOS_ALTOOL_UPLOAD_TIMEOUT_SEC:-600}"

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
    local retry_rc
    echo "[ios-upload] WARNING: altool timed out after ${altool_timeout}s"
    echo "[ios-upload] will auto-retry (resume) to avoid the timeout-cutoff → phantom build trap"
    set +e
    retry_upload_on_timeout
    retry_rc=$?
    set -e
    if [[ "${retry_rc}" -eq 0 ]]; then
      exit 0
    fi
    echo "[ios-upload] ERROR: upload could not be completed (retry_rc=${retry_rc}); checking ASC one last time..."
    set +e
    asc_build_visible
    retry_rc=$?
    set -e
    if [[ "${retry_rc}" -eq 0 ]]; then
      echo "[ios-upload] build ${IPA_BUILD_NUMBER} IS visible in ASC — upload actually completed"
      exit 0
    fi
    fail "altool upload failed after retries and build is NOT in ASC; manual upload required: xcrun altool --upload-app -f ${IPA_PATH} --apiKey ... (no timeout)"
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
