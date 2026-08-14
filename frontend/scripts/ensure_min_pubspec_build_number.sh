#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PUBSPEC_FILE="${PUBSPEC_FILE:-${FRONTEND_ROOT}/pubspec.yaml}"
MIN_BUILD_NUMBER="${1:-}"

fail() {
  echo "[version-build-floor] ERROR: $*" >&2
  exit 1
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
  local current_version version_name build_number next_version

  [[ -f "${PUBSPEC_FILE}" ]] || fail "pubspec not found: ${PUBSPEC_FILE}"
  [[ "${MIN_BUILD_NUMBER}" =~ ^[1-9][0-9]*$ ]] || \
    fail "min build number must be a positive integer, got: ${MIN_BUILD_NUMBER:-<empty>}"

  current_version="$(extract_current_version)"
  [[ -n "${current_version}" ]] || fail "cannot find version line in ${PUBSPEC_FILE}"
  [[ "${current_version}" =~ ^([0-9]+\.[0-9]+\.[0-9]+)\+([0-9]+)$ ]] || \
    fail "unsupported version format: ${current_version}"

  version_name="${BASH_REMATCH[1]}"
  build_number="${BASH_REMATCH[2]}"

  if (( build_number >= MIN_BUILD_NUMBER )); then
    echo "[version-build-floor] keep ${current_version} (min=${MIN_BUILD_NUMBER})"
    return
  fi

  next_version="${version_name}+${MIN_BUILD_NUMBER}"
  update_pubspec_version_line "${next_version}"
  echo "[version-build-floor] ${current_version} -> ${next_version} (min=${MIN_BUILD_NUMBER})"
}

main "$@"
