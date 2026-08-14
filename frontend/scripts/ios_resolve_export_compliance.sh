#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"

IPA_PATH="${1:-${FRONTEND_ROOT}/build/ios/ipa/Grix.ipa}"
IOS_EXPORT_COMPLIANCE_WAIT_TIMEOUT_SEC="${IOS_EXPORT_COMPLIANCE_WAIT_TIMEOUT_SEC:-300}"
IOS_EXPORT_COMPLIANCE_WAIT_INTERVAL_SEC="${IOS_EXPORT_COMPLIANCE_WAIT_INTERVAL_SEC:-15}"
ASC_API_BASE="https://api.appstoreconnect.apple.com"

IOS_BUNDLE_ID=""
IPA_SHORT_VERSION=""
IPA_BUILD_NUMBER=""
ASC_JWT_TOKEN=""
TARGET_BUILD_ID=""
TARGET_USES_NON_EXEMPT_ENCRYPTION=""

log() {
  echo "[ios-compliance] $*" >&2
}

fail() {
  echo "[ios-compliance] ERROR: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

require_env() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "missing required env: ${name}"
}

validate_positive_int() {
  local name="$1"
  local value="$2"
  [[ "${value}" =~ ^[1-9][0-9]*$ ]] || fail "${name} must be a positive integer, got: ${value}"
}

normalize_bool() {
  local raw
  raw="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  case "${raw}" in
    true|1|yes)
      printf 'true'
      ;;
    false|0|no)
      printf 'false'
      ;;
    *)
      fail "invalid boolean value: ${1}"
      ;;
  esac
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

asc_get() {
  local path="$1"
  shift
  curl -sS \
    --retry 5 \
    --retry-delay 5 \
    --retry-all-errors \
    --connect-timeout 15 \
    --max-time 90 \
    --get "${ASC_API_BASE}${path}" \
    -H "Authorization: Bearer ${ASC_JWT_TOKEN}" \
    "$@"
}

load_ipa_metadata() {
  local tmp_plist raw_non_exempt

  tmp_plist="$(mktemp)"
  if ! unzip -p "${IPA_PATH}" "Payload/*.app/Info.plist" >"${tmp_plist}" 2>/dev/null; then
    rm -f "${tmp_plist}"
    fail "failed to read Info.plist from ipa: ${IPA_PATH}"
  fi

  IPA_SHORT_VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "${tmp_plist}" 2>/dev/null || true)"
  IPA_BUILD_NUMBER="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "${tmp_plist}" 2>/dev/null || true)"
  IOS_BUNDLE_ID="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "${tmp_plist}" 2>/dev/null || true)"
  raw_non_exempt="$(/usr/libexec/PlistBuddy -c 'Print :ITSAppUsesNonExemptEncryption' "${tmp_plist}" 2>/dev/null || true)"

  rm -f "${tmp_plist}"

  [[ -n "${IPA_SHORT_VERSION}" ]] || fail "cannot read CFBundleShortVersionString from ipa"
  [[ -n "${IPA_BUILD_NUMBER}" ]] || fail "cannot read CFBundleVersion from ipa"
  [[ -n "${IOS_BUNDLE_ID}" ]] || fail "cannot read CFBundleIdentifier from ipa"
  [[ -n "${raw_non_exempt}" ]] || fail "cannot read ITSAppUsesNonExemptEncryption from ipa (set it in frontend/ios/Runner/Info.plist)"

  TARGET_USES_NON_EXEMPT_ENCRYPTION="$(normalize_bool "${raw_non_exempt}")"
}

load_app_id() {
  local response

  response="$(asc_get "/v1/apps" \
    --data-urlencode "filter[bundleId]=${IOS_BUNDLE_ID}" \
    --data-urlencode "limit=1")"

  APP_ID="$(printf '%s' "${response}" | jq -r '.data[0].id // empty')"
  [[ -n "${APP_ID}" ]] || fail "app not found for bundle id: ${IOS_BUNDLE_ID}"
}

wait_for_valid_build_id() {
  local deadline response build_id state

  deadline=$((SECONDS + IOS_EXPORT_COMPLIANCE_WAIT_TIMEOUT_SEC))
  while true; do
    response="$(asc_get "/v1/builds" \
      --data-urlencode "filter[app]=${APP_ID}" \
      --data-urlencode "filter[version]=${IPA_BUILD_NUMBER}" \
      --data-urlencode "filter[preReleaseVersion.version]=${IPA_SHORT_VERSION}" \
      --data-urlencode "sort=-uploadedDate" \
      --data-urlencode "limit=1")"

    build_id="$(printf '%s' "${response}" | jq -r '.data[0].id // empty')"
    state="$(printf '%s' "${response}" | jq -r '.data[0].attributes.processingState // empty')"

    if [[ -n "${build_id}" ]]; then
      case "${state}" in
        VALID)
          TARGET_BUILD_ID="${build_id}"
          return
          ;;
        FAILED|INVALID)
          fail "build processing failed: state=${state}, version=${IPA_SHORT_VERSION}, build=${IPA_BUILD_NUMBER}"
          ;;
        *)
          log "build is processing (state=${state:-unknown}), waiting..."
          ;;
      esac
    else
      log "build not visible yet (version=${IPA_SHORT_VERSION}, build=${IPA_BUILD_NUMBER}), waiting..."
    fi

    if (( SECONDS >= deadline )); then
      log "timed out waiting for build to become VALID (timeout=${IOS_EXPORT_COMPLIANCE_WAIT_TIMEOUT_SEC}s), IPA already uploaded — Apple will process independently"
      exit 2
    fi

    sleep "${IOS_EXPORT_COMPLIANCE_WAIT_INTERVAL_SEC}"
    ASC_JWT_TOKEN="$(generate_jwt)"
  done
}

get_build_non_exempt_value() {
  local response

  response="$(asc_get "/v1/builds/${TARGET_BUILD_ID}" --data-urlencode "fields[builds]=usesNonExemptEncryption,processingState,version")"
  printf '%s' "${response}" | jq -r 'if (.data.attributes | has("usesNonExemptEncryption")) then (.data.attributes.usesNonExemptEncryption | tostring) else empty end'
}

wait_for_build_non_exempt_value() {
  local deadline value

  deadline=$((SECONDS + IOS_EXPORT_COMPLIANCE_WAIT_TIMEOUT_SEC))
  while true; do
    ASC_JWT_TOKEN="$(generate_jwt)"
    value="$(get_build_non_exempt_value)"
    if [[ -n "${value}" ]]; then
      printf '%s' "${value}"
      return
    fi

    if (( SECONDS >= deadline )); then
      log "timed out waiting for build usesNonExemptEncryption to be populated"
      exit 2
    fi

    log "usesNonExemptEncryption still empty after update, waiting..."
    sleep "${IOS_EXPORT_COMPLIANCE_WAIT_INTERVAL_SEC}"
  done
}

set_build_non_exempt_value() {
  local payload response_file http_code error_detail error_detail_lc

  payload="$(jq -n \
    --arg build_id "${TARGET_BUILD_ID}" \
    --argjson uses_non_exempt "${TARGET_USES_NON_EXEMPT_ENCRYPTION}" \
    '{data:{type:"builds",id:$build_id,attributes:{usesNonExemptEncryption:$uses_non_exempt}}}')"

  response_file="$(mktemp)"
  http_code="$(
    curl -sS \
      --retry 5 \
      --retry-delay 5 \
      --retry-all-errors \
      --connect-timeout 15 \
      --max-time 90 \
      -X PATCH "${ASC_API_BASE}/v1/builds/${TARGET_BUILD_ID}" \
      -H "Authorization: Bearer ${ASC_JWT_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "${payload}" \
      -o "${response_file}" \
      -w "%{http_code}"
  )"

  if [[ "${http_code}" =~ ^2 ]]; then
    rm -f "${response_file}"
    return
  fi

  error_detail="$(jq -r '.errors[0].detail // empty' "${response_file}" 2>/dev/null || true)"
  error_detail_lc="$(printf '%s' "${error_detail}" | tr '[:upper:]' '[:lower:]')"
  if [[ "${http_code}" == "409" ]] && [[ "${error_detail_lc}" == *"already set"* ]]; then
    rm -f "${response_file}"
    return
  fi

  log "App Store Connect response: $(cat "${response_file}")"
  rm -f "${response_file}"
  fail "failed to set usesNonExemptEncryption (http=${http_code})"
}

main() {
  local current_non_exempt

  require_unified_release_call "ios-compliance" "./scripts/release.sh mobile-ios-ipa"

  require_cmd xcrun
  require_cmd jq
  require_cmd curl
  require_cmd unzip
  require_cmd /usr/libexec/PlistBuddy
  require_env APPSTORE_API_KEY_ID
  require_env APPSTORE_API_ISSUER_ID

  validate_positive_int "IOS_EXPORT_COMPLIANCE_WAIT_TIMEOUT_SEC" "${IOS_EXPORT_COMPLIANCE_WAIT_TIMEOUT_SEC}"
  validate_positive_int "IOS_EXPORT_COMPLIANCE_WAIT_INTERVAL_SEC" "${IOS_EXPORT_COMPLIANCE_WAIT_INTERVAL_SEC}"
  [[ -f "${IPA_PATH}" ]] || fail "ipa not found: ${IPA_PATH}"

  ASC_JWT_TOKEN="$(generate_jwt)"
  load_ipa_metadata
  load_app_id
  wait_for_valid_build_id

  ASC_JWT_TOKEN="$(generate_jwt)"
  current_non_exempt="$(get_build_non_exempt_value)"

  log "target app=${IOS_BUNDLE_ID}, version=${IPA_SHORT_VERSION}, build=${IPA_BUILD_NUMBER}, usesNonExemptEncryption=${TARGET_USES_NON_EXEMPT_ENCRYPTION}"

  if [[ -z "${current_non_exempt}" ]]; then
    log "usesNonExemptEncryption is unset, patching build..."
    set_build_non_exempt_value
    current_non_exempt="$(wait_for_build_non_exempt_value)"
  fi

  current_non_exempt="$(normalize_bool "${current_non_exempt}")"
  if [[ "${current_non_exempt}" != "${TARGET_USES_NON_EXEMPT_ENCRYPTION}" ]]; then
    fail "build usesNonExemptEncryption=${current_non_exempt}, expected ${TARGET_USES_NON_EXEMPT_ENCRYPTION}; fix Info.plist or update compliance policy"
  fi

  log "export compliance resolved (usesNonExemptEncryption=${current_non_exempt})"
}

main "$@"
