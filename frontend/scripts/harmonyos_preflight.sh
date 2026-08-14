#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

FAILED=0
WARNINGS=0

log() {
  echo "[harmonyos-preflight] $*"
}

ok() {
  log "ok: $*"
}

fail() {
  log "missing: $*" >&2
  FAILED=1
}

warn() {
  log "warn: $*" >&2
  WARNINGS=$((WARNINGS + 1))
}

command_path() {
  command -v "$1" 2>/dev/null || true
}

check_command() {
  local name="$1"
  local label="$2"
  local path
  path="$(command_path "${name}")"
  if [ -n "${path}" ]; then
    ok "${label}: ${path}"
    return
  fi
  fail "${label} not found on PATH"
}

check_command_either() {
  local left="$1"
  local right="$2"
  local label="$3"
  local left_path
  local right_path

  left_path="$(command_path "${left}")"
  if [ -n "${left_path}" ]; then
    ok "${label}: ${left_path}"
    return
  fi

  right_path="$(command_path "${right}")"
  if [ -n "${right_path}" ]; then
    ok "${label}: ${right_path}"
    return
  fi

  fail "${label} not found on PATH (${left} / ${right})"
}

check_java() {
  local java_path
  java_path="$(command_path java)"
  if [ -z "${java_path}" ]; then
    fail "java not found on PATH"
    return
  fi

  local version_line
  version_line="$(java -version 2>&1 | head -n 1 || true)"
  if [[ "${version_line}" == *"Unable to locate a Java Runtime"* ]]; then
    fail "java exists at ${java_path}, but no usable Java Runtime is installed"
    return
  fi

  ok "java: ${java_path} (${version_line})"

  if [[ "${version_line}" != *"17."* && "${version_line}" != *"\"17"* ]]; then
    warn "HarmonyOS toolchain is typically validated with Java 17; current version may be incompatible"
  fi
}

detect_deveco_sdk_home() {
  if [ -n "${OHOS_SDK_HOME:-}" ] && [ -d "${OHOS_SDK_HOME}" ]; then
    echo "OHOS_SDK_HOME:${OHOS_SDK_HOME}"
    return 0
  fi

  if [ -n "${DEVECO_SDK_HOME:-}" ] && [ -d "${DEVECO_SDK_HOME}" ]; then
    echo "DEVECO_SDK_HOME:${DEVECO_SDK_HOME}"
    return 0
  fi

  local candidates=(
    "${HOME}/development/ohos/sdk/openharmony"
    "/Applications/DevEco-Studio.app/Contents/sdk"
    "${HOME}/Applications/DevEco-Studio.app/Contents/sdk"
  )
  local candidate
  for candidate in "${candidates[@]}"; do
    if [ -d "${candidate}" ]; then
      echo "${candidate}"
      return 0
    fi
  done

  return 1
}

check_deveco_sdk_home() {
  local detected raw_name sdk_home
  detected="$(detect_deveco_sdk_home || true)"
  if [ -n "${detected}" ]; then
    raw_name="${detected%%:*}"
    sdk_home="${detected#*:}"
    ok "${raw_name}: ${sdk_home}"
    if [ "${raw_name}" = "OHOS_SDK_HOME" ] && [ -z "${OHOS_SDK_HOME:-}" ]; then
      warn "OHOS_SDK_HOME is not exported in the current shell; using detected path only for reporting"
    fi
    if [ "${raw_name}" = "DEVECO_SDK_HOME" ] && [ -z "${DEVECO_SDK_HOME:-}" ]; then
      warn "DEVECO_SDK_HOME is not exported in the current shell; using detected path only for reporting"
    fi
    return
  fi

  fail "OHOS_SDK_HOME / DEVECO_SDK_HOME is not set and no HarmonyOS SDK directory was auto-detected"
}

version_lt() {
  local left="$1"
  local right="$2"

  if [ "${left}" = "${right}" ]; then
    return 1
  fi

  [ "$(printf '%s\n%s\n' "${left}" "${right}" | sort -V | head -n 1)" = "${left}" ]
}

extract_current_dart_version() {
  flutter --version 2>/dev/null | grep -oE 'Dart( version)? [0-9]+\.[0-9]+\.[0-9]+' | head -n 1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || true
}

extract_required_dart_version() {
  grep -E '^[[:space:]]*sdk:' "${FRONTEND_ROOT}/pubspec.yaml" | head -n 1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || true
}

check_flutter() {
  local flutter_path
  flutter_path="$(command_path flutter)"
  if [ -z "${flutter_path}" ]; then
    fail "flutter not found on PATH"
    return
  fi

  local version_line
  version_line="$(flutter --version 2>/dev/null | head -n 1 || true)"
  ok "flutter: ${flutter_path} (${version_line})"

  local build_help
  build_help="$(flutter build -h 2>/dev/null || true)"

  if printf '%s\n' "${build_help}" | grep -qE '^[[:space:]]+hap[[:space:]]'; then
    ok "flutter build supports hap"
  else
    fail "current flutter does not support 'flutter build hap'; install OpenHarmony Flutter SDK"
  fi

  if printf '%s\n' "${build_help}" | grep -qE '^[[:space:]]+app[[:space:]]'; then
    ok "flutter build supports app"
  else
    fail "current flutter does not support 'flutter build app'; install OpenHarmony Flutter SDK"
  fi

  local current_dart_version required_dart_version
  current_dart_version="$(extract_current_dart_version)"
  required_dart_version="$(extract_required_dart_version)"

  if [ -n "${current_dart_version}" ]; then
    ok "flutter Dart version: ${current_dart_version}"
  fi

  if [ -n "${required_dart_version}" ]; then
    ok "project requires Dart: ${required_dart_version}"
  fi

  if [ -n "${current_dart_version}" ] && [ -n "${required_dart_version}" ] && version_lt "${current_dart_version}" "${required_dart_version}"; then
    fail "current flutter ships Dart ${current_dart_version}, but frontend/pubspec.yaml requires >= ${required_dart_version}"
  fi
}

check_optional_env() {
  local name="$1"
  local recommendation="$2"
  if [ -n "${!name:-}" ]; then
    ok "${name}: ${!name}"
    return
  fi
  warn "${name} is not set (${recommendation})"
}

check_project_layout() {
  if [ -d "${FRONTEND_ROOT}/ohos" ]; then
    ok "project has ohos platform directory"
  else
    warn "project has no ohos/ directory yet; run 'flutter create --platforms ohos .' under OpenHarmony Flutter SDK"
  fi
}

main() {
  log "frontend root: ${FRONTEND_ROOT}"

  check_flutter
  check_java
  check_deveco_sdk_home
  check_command node "node"
  check_command ohpm "ohpm"
  check_command hdc "hdc"
  check_command_either hvigorw hvigor "hvigor"

  check_optional_env JAVA_HOME "recommended to point at Java 17"
  check_optional_env OHOS_SDK_HOME "recommended for OpenHarmony command-line SDK installs"
  check_optional_env DEVECO_SDK_HOME "recommended for DevEco Studio based SDK installs"
  check_optional_env NODE_HOME "recommended for hvigor/local.properties generation"
  check_optional_env PUB_HOSTED_URL "recommended for mainland network access"
  check_optional_env FLUTTER_STORAGE_BASE_URL "recommended for mainland network access"
  check_optional_env FLUTTER_GIT_URL "recommended when using OpenHarmony Flutter fork"

  check_project_layout

  if [ "${FAILED}" -ne 0 ]; then
    log "result: FAILED (${WARNINGS} warning(s))" >&2
    log "next: fix the missing items above, then rerun this script" >&2
    exit 1
  fi

  log "result: PASS (${WARNINGS} warning(s))"
  log "next: run 'flutter doctor -v', then 'flutter pub get', then 'flutter build hap --debug' in ${FRONTEND_ROOT}"
}

main "$@"
