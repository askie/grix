#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PUBSPEC_FILE="${PUBSPEC_FILE:-${FRONTEND_ROOT}/pubspec.yaml}"
BUMP_LEVEL="${1:-build}"

fail() {
  echo "[version-bump] ERROR: $*" >&2
  exit 1
}

validate_bump_level() {
  case "${BUMP_LEVEL}" in
    build|patch|minor|major)
      ;;
    *)
      fail "bump level must be one of: build, patch, minor, major; got: ${BUMP_LEVEL}"
      ;;
  esac
}

extract_current_version() {
  awk '
    /^[[:space:]]*version:[[:space:]]*[0-9]+\.[0-9]+\.[0-9]+\+[0-9]+[[:space:]]*$/ {
      print $2
      exit
    }
  ' "${PUBSPEC_FILE}"
}

update_pubspec_version_line() {
  local new_version="$1"
  local tmp_file

  tmp_file="$(mktemp "${PUBSPEC_FILE}.tmp.XXXXXX")"

  if ! awk -v next_version="${new_version}" '
    BEGIN {
      updated = 0
    }
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
    END {
      if (updated == 0) {
        exit 42
      }
    }
  ' "${PUBSPEC_FILE}" >"${tmp_file}"; then
    rm -f "${tmp_file}"
    fail "failed to rewrite ${PUBSPEC_FILE}"
  fi

  mv "${tmp_file}" "${PUBSPEC_FILE}"
}

main() {
  local current_version version_name build_number
  local major minor patch next_build next_version_name next_version

  [[ -f "${PUBSPEC_FILE}" ]] || fail "pubspec not found: ${PUBSPEC_FILE}"
  validate_bump_level

  current_version="$(extract_current_version)"
  [[ -n "${current_version}" ]] || fail "cannot find version line in ${PUBSPEC_FILE}"
  [[ "${current_version}" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)\+([0-9]+)$ ]] || \
    fail "unsupported version format: ${current_version}"

  version_name="${current_version%%+*}"
  build_number="${current_version##*+}"

  IFS='.' read -r major minor patch <<<"${version_name}"
  next_build="$((build_number + 1))"

  case "${BUMP_LEVEL}" in
    build)
      ;;
    patch)
      patch="$((patch + 1))"
      ;;
    minor)
      minor="$((minor + 1))"
      patch="0"
      ;;
    major)
      major="$((major + 1))"
      minor="0"
      patch="0"
      ;;
  esac

  next_version_name="${major}.${minor}.${patch}"
  next_version="${next_version_name}+${next_build}"

  update_pubspec_version_line "${next_version}"
  echo "[version-bump] ${current_version} -> ${next_version} (level=${BUMP_LEVEL})"
}

main "$@"
