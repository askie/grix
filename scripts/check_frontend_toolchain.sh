#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_DIR="${ROOT_DIR}/frontend"
PUBSPEC_FILE="${FRONTEND_DIR}/pubspec.yaml"
PBXPROJ_FILE="${FRONTEND_DIR}/ios/Runner.xcodeproj/project.pbxproj"

EXPECTED_FLUTTER_VERSION="3.41.6"
EXPECTED_DART_VERSION="3.11.4"
EXPECTED_XCODE_VERSION="26.3"
EXPECTED_XCODE_BUILD="17C529"
EXPECTED_IOS_SDK_VERSION="26.2"
EXPECTED_COCOAPODS_VERSION="1.16.2"
EXPECTED_RUBY_VERSION_PREFIX="2.6.10"
EXPECTED_IOS_DEPLOYMENT_TARGET="15.0"

PASS_COUNT=0
FAIL_COUNT=0

log() {
  echo "[toolchain-check] $*"
}

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  printf '[toolchain-check] PASS %-28s %s\n' "$1" "$2"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  printf '[toolchain-check] FAIL %-28s %s\n' "$1" "$2" >&2
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    fail "command:${cmd}" "missing command"
    return 1
  fi
  pass "command:${cmd}" "found"
}

assert_equals() {
  local name="$1"
  local expected="$2"
  local actual="$3"
  if [[ "${actual}" == "${expected}" ]]; then
    pass "${name}" "expected=${expected}, actual=${actual}"
  else
    fail "${name}" "expected=${expected}, actual=${actual}"
  fi
}

assert_prefix() {
  local name="$1"
  local expected_prefix="$2"
  local actual="$3"
  if [[ "${actual}" == "${expected_prefix}"* ]]; then
    pass "${name}" "expected_prefix=${expected_prefix}, actual=${actual}"
  else
    fail "${name}" "expected_prefix=${expected_prefix}, actual=${actual}"
  fi
}

assert_pubspec_pin() {
  local dep="$1"
  local regex="$2"
  if grep -Eq "${regex}" "${PUBSPEC_FILE}"; then
    pass "pubspec:${dep}" "pinned"
  else
    fail "pubspec:${dep}" "missing required pin"
  fi
}

assert_file_exists() {
  local name="$1"
  local path="$2"
  if [[ -f "${path}" ]]; then
    pass "${name}" "${path}"
  else
    fail "${name}" "missing file: ${path}"
  fi
}

main() {
  log "checking frontend toolchain baseline"

  require_cmd flutter
  require_cmd xcodebuild
  require_cmd xcrun
  require_cmd pod
  require_cmd ruby

  assert_file_exists "pubspec" "${PUBSPEC_FILE}"
  assert_file_exists "pbxproj" "${PBXPROJ_FILE}"

  local flutter_machine
  flutter_machine="$(flutter --version --machine)"
  local flutter_version
  flutter_version="$(printf '%s\n' "${flutter_machine}" | awk -F'"' '$2=="flutterVersion"{print $4; exit}')"
  local dart_version
  dart_version="$(printf '%s\n' "${flutter_machine}" | awk -F'"' '$2=="dartSdkVersion"{print $4; exit}')"
  assert_equals "flutter" "${EXPECTED_FLUTTER_VERSION}" "${flutter_version}"
  assert_equals "dart" "${EXPECTED_DART_VERSION}" "${dart_version}"

  local xcode_out xcode_version xcode_build
  xcode_out="$(xcodebuild -version)"
  xcode_version="$(printf '%s\n' "${xcode_out}" | awk 'NR==1{print $2}')"
  xcode_build="$(printf '%s\n' "${xcode_out}" | awk 'NR==2{print $3}')"
  assert_equals "xcode" "${EXPECTED_XCODE_VERSION}" "${xcode_version}"
  assert_equals "xcode_build" "${EXPECTED_XCODE_BUILD}" "${xcode_build}"

  local ios_sdk_version
  ios_sdk_version="$(xcrun --sdk iphoneos --show-sdk-version)"
  assert_equals "iphoneos_sdk" "${EXPECTED_IOS_SDK_VERSION}" "${ios_sdk_version}"

  local pod_version
  pod_version="$(pod --version)"
  assert_equals "cocoapods" "${EXPECTED_COCOAPODS_VERSION}" "${pod_version}"

  local ruby_version
  ruby_version="$(ruby -v | awk '{print $2}')"
  assert_prefix "ruby" "${EXPECTED_RUBY_VERSION_PREFIX}" "${ruby_version}"

  local deployment_targets_list deployment_target_count deployment_target
  deployment_targets_list="$(
    grep -o 'IPHONEOS_DEPLOYMENT_TARGET = [0-9.]*;' "${PBXPROJ_FILE}" \
      | sed -E 's/.*= ([0-9.]+);/\1/' \
      | sort -u
  )"
  deployment_target_count="$(
    printf '%s\n' "${deployment_targets_list}" | sed '/^$/d' | wc -l | tr -d ' '
  )"

  if [[ "${deployment_target_count}" -ne 1 ]]; then
    fail "ios_deployment_target" "found values: ${deployment_targets_list:-none}"
  else
    deployment_target="$(printf '%s\n' "${deployment_targets_list}" | head -n1)"
    assert_equals "ios_deployment_target" "${EXPECTED_IOS_DEPLOYMENT_TARGET}" "${deployment_target}"
  fi

  assert_pubspec_pin "intl" '^[[:space:]]+intl:[[:space:]]+\^0\.20\.2$'
  assert_pubspec_pin "flutter_math_fork" '^[[:space:]]+flutter_math_fork:[[:space:]]+\^0\.7\.4$'
  assert_pubspec_pin "app_links" '^[[:space:]]+app_links:[[:space:]]+6\.4\.0$'
  assert_pubspec_pin "mobile_scanner" '^[[:space:]]+mobile_scanner:[[:space:]]+5\.2\.3$'

  log "summary: PASS=${PASS_COUNT}, FAIL=${FAIL_COUNT}"
  if [[ "${FAIL_COUNT}" -gt 0 ]]; then
    exit 1
  fi
  log "toolchain baseline check passed"
}

main "$@"
