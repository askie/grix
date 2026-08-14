#!/usr/bin/env bash
set -euo pipefail

# admin 端 pubspec 版本号 bump 脚本
# 用法: ./admin/scripts/bump_pubspec_version.sh [build|patch|minor|major]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ADMIN_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PUBSPEC_FILE="${ADMIN_ROOT}/pubspec.yaml"
BUMP_LEVEL="${1:-build}"

fail() {
  echo "[admin-version-bump] ERROR: $*" >&2
  exit 1
}

validate_bump_level() {
  case "${BUMP_LEVEL}" in
    build|patch|minor|major) ;;
    *) fail "必须是: build, patch, minor, major; 收到: ${BUMP_LEVEL}" ;;
  esac
}

extract_current_version() {
  awk '/^[[:space:]]*version:[[:space:]]*[0-9]+\.[0-9]+\.[0-9]+\+[0-9]+[[:space:]]*$/ { print $2; exit }' "${PUBSPEC_FILE}"
}

update_pubspec_version_line() {
  local new_version="$1"
  local tmp_file
  tmp_file="$(mktemp "${PUBSPEC_FILE}.tmp.XXXXXX")"

  if ! awk -v next_version="${new_version}" '
    BEGIN { updated = 0 }
    {
      if (updated == 0 && $0 ~ /^[[:space:]]*version:[[:space:]]*[0-9]+\.[0-9]+\.[0-9]+\+[0-9]+[[:space:]]*$/) {
        match($0, /^[[:space:]]*/)
        indent = substr($0, RSTART, RLENGTH)
        print indent "version: " next_version
        updated = 1
        next
      }
      print
    }
    END { if (updated == 0) exit 42 }
  ' "${PUBSPEC_FILE}" >"${tmp_file}"; then
    rm -f "${tmp_file}"
    fail "改写 ${PUBSPEC_FILE} 失败"
  fi

  mv "${tmp_file}" "${PUBSPEC_FILE}"
}

main() {
  [[ -f "${PUBSPEC_FILE}" ]] || fail "pubspec 不存在: ${PUBSPEC_FILE}"
  validate_bump_level

  local current_version
  current_version="$(extract_current_version)"
  [[ -n "${current_version}" ]] || fail "未找到 version 行: ${PUBSPEC_FILE}"
  [[ "${current_version}" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)\+([0-9]+)$ ]] || \
    fail "版本格式不支持: ${current_version}"

  local major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}"
  local build_number="${BASH_REMATCH[4]}"
  local next_build=$((build_number + 1))

  case "${BUMP_LEVEL}" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
  esac

  local next_version="${major}.${minor}.${patch}+${next_build}"
  update_pubspec_version_line "${next_version}"
  echo "[admin-version-bump] ${current_version} -> ${next_version} (level=${BUMP_LEVEL})"
}

main "$@"
