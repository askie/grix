#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_OUTPUT_ROOT="${SCRIPT_DIR}/../deploy/well-known"
OUTPUT_ROOT="${1:-${WELL_KNOWN_OUTPUT_ROOT:-${DEFAULT_OUTPUT_ROOT}}}"
OUTPUT_DIR="${OUTPUT_ROOT}/.well-known"

required_env_vars=(
  "AIBOT_SERVER_DEEP_LINK_IOS_APP_ID"
  "AIBOT_SERVER_DEEP_LINK_ANDROID_PACKAGE"
  "AIBOT_SERVER_DEEP_LINK_ANDROID_SHA256_CERTS"
)

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf "%s" "${value}"
}

for env_var in "${required_env_vars[@]}"; do
  if [[ -z "${!env_var:-}" ]]; then
    printf "error: missing required env %s\n" "${env_var}" >&2
    exit 1
  fi
done

ios_app_id="$(trim "${AIBOT_SERVER_DEEP_LINK_IOS_APP_ID}")"
android_package="$(trim "${AIBOT_SERVER_DEEP_LINK_ANDROID_PACKAGE}")"
raw_fingerprints="$(trim "${AIBOT_SERVER_DEEP_LINK_ANDROID_SHA256_CERTS}")"

if [[ "${ios_app_id}" != *.* ]]; then
  printf "error: AIBOT_SERVER_DEEP_LINK_IOS_APP_ID must use TeamID.BundleID format\n" >&2
  exit 1
fi

if [[ -z "${android_package}" ]]; then
  printf "error: AIBOT_SERVER_DEEP_LINK_ANDROID_PACKAGE cannot be empty\n" >&2
  exit 1
fi

IFS=',' read -r -a fingerprint_parts <<< "${raw_fingerprints}"
fingerprints=()
for part in "${fingerprint_parts[@]}"; do
  normalized="$(trim "${part}")"
  if [[ -z "${normalized}" ]]; then
    continue
  fi

  normalized="$(printf "%s" "${normalized}" | tr '[:lower:]' '[:upper:]')"
  if [[ ! "${normalized}" =~ ^([0-9A-F]{2}:){31}[0-9A-F]{2}$ ]]; then
    printf "error: invalid SHA256 fingerprint format: %s\n" "${normalized}" >&2
    exit 1
  fi
  fingerprints+=("${normalized}")
done

if [[ ${#fingerprints[@]} -eq 0 ]]; then
  printf "error: AIBOT_SERVER_DEEP_LINK_ANDROID_SHA256_CERTS must contain at least one fingerprint\n" >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"

aasa_file="${OUTPUT_DIR}/apple-app-site-association"
assetlinks_file="${OUTPUT_DIR}/assetlinks.json"

cat >"${aasa_file}" <<EOF
{
  "applinks": {
    "apps": [],
    "details": [
      {
        "appID": "${ios_app_id}",
        "paths": [
          "/u/*"
        ]
      }
    ]
  }
}
EOF

{
  printf "[\n"
  printf "  {\n"
  printf "    \"relation\": [\"delegate_permission/common.handle_all_urls\"],\n"
  printf "    \"target\": {\n"
  printf "      \"namespace\": \"android_app\",\n"
  printf "      \"package_name\": \"%s\",\n" "${android_package}"
  printf "      \"sha256_cert_fingerprints\": [\n"

  for i in "${!fingerprints[@]}"; do
    suffix=","
    if [[ "${i}" -eq $((${#fingerprints[@]} - 1)) ]]; then
      suffix=""
    fi
    printf "        \"%s\"%s\n" "${fingerprints[${i}]}" "${suffix}"
  done

  printf "      ]\n"
  printf "    }\n"
  printf "  }\n"
  printf "]\n"
} >"${assetlinks_file}"

printf "generated files:\n"
printf "  %s\n" "${aasa_file}"
printf "  %s\n" "${assetlinks_file}"

