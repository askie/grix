#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"

IPA_PATH="${1:-${FRONTEND_ROOT}/build/ios/ipa/Grix.ipa}"
IOS_BUNDLE_ID="${IOS_BUNDLE_ID:-pub.dhf.grix}"
TESTFLIGHT_GROUP_NAME="${TESTFLIGHT_GROUP_NAME:-dhf}"
TESTFLIGHT_WAIT_TIMEOUT_SEC="${TESTFLIGHT_WAIT_TIMEOUT_SEC:-300}"
TESTFLIGHT_WAIT_INTERVAL_SEC="${TESTFLIGHT_WAIT_INTERVAL_SEC:-15}"
ASC_API_BASE="https://api.appstoreconnect.apple.com"

APP_ID=""
BETA_GROUP_ID=""
IPA_SHORT_VERSION=""
IPA_BUILD_NUMBER=""
ASC_JWT_TOKEN=""
TARGET_BUILD_ID=""

log() {
  echo "[ios-distribute] $*"
}

fail() {
  echo "[ios-distribute] ERROR: $*" >&2
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

load_ipa_version_metadata() {
  local tmp_plist

  tmp_plist="$(mktemp)"
  if ! unzip -p "${IPA_PATH}" "Payload/*.app/Info.plist" >"${tmp_plist}" 2>/dev/null; then
    rm -f "${tmp_plist}"
    fail "failed to read Info.plist from ipa: ${IPA_PATH}"
  fi

  IPA_SHORT_VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "${tmp_plist}" 2>/dev/null || true)"
  IPA_BUILD_NUMBER="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "${tmp_plist}" 2>/dev/null || true)"
  rm -f "${tmp_plist}"

  [[ -n "${IPA_SHORT_VERSION}" ]] || fail "cannot read CFBundleShortVersionString from ipa"
  [[ -n "${IPA_BUILD_NUMBER}" ]] || fail "cannot read CFBundleVersion from ipa"
}

load_app_id() {
  local response

  response="$(asc_get "/v1/apps" \
    --data-urlencode "filter[bundleId]=${IOS_BUNDLE_ID}" \
    --data-urlencode "limit=1")"
  APP_ID="$(printf '%s' "${response}" | jq -r '.data[0].id // empty')"
  [[ -n "${APP_ID}" ]] || fail "app not found for bundle id: ${IOS_BUNDLE_ID}"
}

load_beta_group_id() {
  local response is_internal

  response="$(asc_get "/v1/betaGroups" \
    --data-urlencode "filter[app]=${APP_ID}" \
    --data-urlencode "filter[name]=${TESTFLIGHT_GROUP_NAME}" \
    --data-urlencode "limit=1")"
  BETA_GROUP_ID="$(printf '%s' "${response}" | jq -r '.data[0].id // empty')"
  is_internal="$(printf '%s' "${response}" | jq -r '.data[0].attributes.isInternalGroup // empty')"

  [[ -n "${BETA_GROUP_ID}" ]] || fail "beta group not found: ${TESTFLIGHT_GROUP_NAME}"
  [[ "${is_internal}" == "true" ]] || fail "target beta group is not internal: ${TESTFLIGHT_GROUP_NAME} (auto review submission is disabled)"
}

wait_for_valid_build_id() {
  local deadline response build_id state

  deadline=$((SECONDS + TESTFLIGHT_WAIT_TIMEOUT_SEC))
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
      log "timed out waiting for build to become VALID (timeout=${TESTFLIGHT_WAIT_TIMEOUT_SEC}s), IPA already uploaded — Apple will process independently"
      exit 2
    fi

    sleep "${TESTFLIGHT_WAIT_INTERVAL_SEC}"
    ASC_JWT_TOKEN="$(generate_jwt)"
  done
}

attach_build_to_group() {
  local build_id="$1"
  local payload response_file http_code error_detail error_detail_lc

  payload="$(jq -n --arg id "${build_id}" '{data:[{type:"builds",id:$id}]}')"
  response_file="$(mktemp)"

  http_code="$(
    curl -sS \
      --retry 5 \
      --retry-delay 5 \
      --retry-all-errors \
      --connect-timeout 15 \
      --max-time 90 \
      -X POST "${ASC_API_BASE}/v1/betaGroups/${BETA_GROUP_ID}/relationships/builds" \
      -H "Authorization: Bearer ${ASC_JWT_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "${payload}" \
      -o "${response_file}" \
      -w "%{http_code}"
  )"

  if [[ "${http_code}" =~ ^2 ]]; then
    rm -f "${response_file}"
    log "build added to TestFlight group '${TESTFLIGHT_GROUP_NAME}'"
    return
  fi

  error_detail="$(jq -r '.errors[0].detail // empty' "${response_file}" 2>/dev/null || true)"
  error_detail_lc="$(printf '%s' "${error_detail}" | tr '[:upper:]' '[:lower:]')"
  if [[ "${http_code}" == "409" ]] && [[ "${error_detail_lc}" == *"already"* ]]; then
    rm -f "${response_file}"
    log "build already exists in TestFlight group '${TESTFLIGHT_GROUP_NAME}'"
    return
  fi

  if [[ "${http_code}" == "422" ]] && [[ "${error_detail}" == *"Cannot add internal group to a build."* ]]; then
    rm -f "${response_file}"
    log "internal TestFlight group '${TESTFLIGHT_GROUP_NAME}' does not support explicit build assignment via API; build is available after processing"
    return
  fi

  log "App Store Connect response: $(cat "${response_file}")"
  rm -f "${response_file}"
  fail "failed to add build to TestFlight group (http=${http_code})"
}

main() {
  require_unified_release_call "ios-distribute" "./scripts/release.sh mobile-ios-ipa"

  require_cmd xcrun
  require_cmd jq
  require_cmd curl
  require_cmd unzip
  require_cmd /usr/libexec/PlistBuddy
  require_env APPSTORE_API_KEY_ID
  require_env APPSTORE_API_ISSUER_ID

  validate_positive_int "TESTFLIGHT_WAIT_TIMEOUT_SEC" "${TESTFLIGHT_WAIT_TIMEOUT_SEC}"
  validate_positive_int "TESTFLIGHT_WAIT_INTERVAL_SEC" "${TESTFLIGHT_WAIT_INTERVAL_SEC}"
  [[ -f "${IPA_PATH}" ]] || fail "ipa not found: ${IPA_PATH}"

  ASC_JWT_TOKEN="$(generate_jwt)"
  load_ipa_version_metadata
  load_app_id
  load_beta_group_id

  log "target app=${IOS_BUNDLE_ID}, group=${TESTFLIGHT_GROUP_NAME}, version=${IPA_SHORT_VERSION}, build=${IPA_BUILD_NUMBER}"
  wait_for_valid_build_id

  ASC_JWT_TOKEN="$(generate_jwt)"
  attach_build_to_group "${TARGET_BUILD_ID}"
}

main "$@"
