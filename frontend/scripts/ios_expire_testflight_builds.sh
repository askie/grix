#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"

ASC_API_BASE="https://api.appstoreconnect.apple.com"

IOS_BUNDLE_ID="${IOS_BUNDLE_ID:-${DEFAULT_IOS_BUNDLE_ID}}"
ASC_JWT_TOKEN=""
APP_ID=""
DRY_RUN=0
TARGET_FILE=""
TARGET_KEYS=()
TARGET_MARKETING_VERSIONS=()
TARGET_BUILD_NUMBERS=()

BEFORE_TARGET=""
BEFORE_MARKETING_VERSION=""
BEFORE_BUILD_NUMBER=""
BEFORE_BUILD_NUMBER_ONLY=""
BEFORE_BUILD_IDS=()
BEFORE_BUILD_MARKETING_VERSIONS=()
BEFORE_BUILD_NUMBERS=()
BEFORE_BUILD_EXPIRED_FLAGS=()
BEFORE_BUILD_STATES=()
BEFORE_BUILD_UPLOADED_DATES=()

LOOKUP_BUILD_ID=""
LOOKUP_BUILD_EXPIRED=""
LOOKUP_BUILD_PROCESSING_STATE=""
LOOKUP_BUILD_UPLOADED_DATE=""

log() {
  echo "[ios-expire-builds] $*"
}

fail() {
  echo "[ios-expire-builds] ERROR: $*" >&2
  exit 1
}

json_compact() {
  local input="$1"
  local compact

  compact="$(printf '%s' "${input}" | jq -c '.' 2>/dev/null || true)"
  if [[ -n "${compact}" ]]; then
    printf '%s' "${compact}"
    return
  fi

  printf '%s' "${input}"
}

asc_error_summary() {
  local response="$1"
  local summary

  summary="$(printf '%s' "${response}" | jq -r '
    if (.errors | type) == "array" and (.errors | length) > 0 then
      .errors
      | map(
          [
            (.status // "unknown"),
            (.code // "unknown"),
            (.title // "unknown"),
            (.detail // "no detail")
          ] | join(" | ")
        )
      | join("; ")
    else
      ""
    end
  ' 2>/dev/null || true)"

  printf '%s' "${summary}"
}

ensure_asc_data_array() {
  local response="$1"
  local context="$2"
  local data_type errors_summary compact

  data_type="$(printf '%s' "${response}" | jq -r 'if has("data") then (.data | type) else "missing" end' 2>/dev/null || true)"
  if [[ "${data_type}" == "array" ]]; then
    return
  fi

  errors_summary="$(asc_error_summary "${response}")"
  if [[ -n "${errors_summary}" ]]; then
    fail "${context}: App Store Connect returned errors: ${errors_summary}"
  fi

  compact="$(json_compact "${response}")"
  fail "${context}: unexpected App Store Connect response, expected data array but got ${data_type}: ${compact}"
}

usage() {
  cat <<'USAGE'
Usage:
  bash frontend/scripts/ios_expire_testflight_builds.sh \
    --bundle-id <bundle_id> \
    --target <marketing_version:build_number> \
    [--target <marketing_version:build_number> ...] \
    [--target-file <file>] \
    [--dry-run]

  bash frontend/scripts/ios_expire_testflight_builds.sh \
    --bundle-id <bundle_id> \
    --before <marketing_version:build_number|build_number> \
    [--dry-run]

  bash frontend/scripts/ios_expire_testflight_builds.sh \
    --bundle-id <bundle_id> \
    <build_number> \
    [--dry-run]

Arguments:
  --bundle-id   iOS bundle id. Default: IOS_BUNDLE_ID env or pub.dhf.grix.
  --target      Exact build selector: MARKETING_VERSION:BUILD_NUMBER.
  --target-file File with one target per line using MARKETING_VERSION:BUILD_NUMBER.
                Empty lines and lines starting with # are ignored.
  --before      Expire all builds strictly before this selector:
                - MARKETING_VERSION:BUILD_NUMBER
                - BUILD_NUMBER (marketing version ignored)
  <build_number> Positional shorthand for --before BUILD_NUMBER.
  --dry-run     Resolve targets only; do not patch ASC.

Rules:
  1) --before/<build_number> and --target/--target-file are mutually exclusive.
  2) MARKETING_VERSION must be numeric segments, e.g. 1 or 1.0 or 1.0.7.
  3) BUILD_NUMBER must be positive integer.

Environment:
  APPSTORE_API_KEY_ID           Required.
  APPSTORE_API_ISSUER_ID        Required.
  APPSTORE_API_PRIVATE_KEYS_DIR Optional. If set, exported to API_PRIVATE_KEYS_DIR.

Examples:
  bash frontend/scripts/ios_expire_testflight_builds.sh \
    --bundle-id pub.dhf.grix \
    --target 1.0.6:10 \
    --target 1.0.6:11

  bash frontend/scripts/ios_expire_testflight_builds.sh \
    --bundle-id pub.dhf.grix \
    --before 1.0.7:25 \
    --dry-run

  bash frontend/scripts/ios_expire_testflight_builds.sh \
    --bundle-id pub.dhf.grix \
    57 \
    --dry-run
USAGE
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

require_env() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "missing required env: ${name}"
}

trim_line() {
  local input="$1"
  printf '%s' "${input}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

validate_marketing_version() {
  local value="$1"
  [[ "${value}" =~ ^[0-9]+(\.[0-9]+)*$ ]] || fail "invalid marketing version: ${value}"
}

validate_build_number() {
  local value="$1"
  [[ "${value}" =~ ^[1-9][0-9]*$ ]] || fail "invalid build number: ${value}"
}

parse_target_pair() {
  local raw="$1"
  local source="$2"
  local marketing_version build_number

  [[ "${raw}" == *:* ]] || fail "invalid target format (${source}): ${raw}, expected MARKETING_VERSION:BUILD_NUMBER"
  marketing_version="${raw%%:*}"
  build_number="${raw#*:}"

  [[ -n "${marketing_version}" ]] || fail "marketing version is empty (${source})"
  [[ -n "${build_number}" ]] || fail "build number is empty (${source})"

  validate_marketing_version "${marketing_version}"
  validate_build_number "${build_number}"

  printf '%s\t%s' "${marketing_version}" "${build_number}"
}

add_target() {
  local raw="$1"
  local source="$2"
  local parsed marketing_version build_number key existing

  parsed="$(parse_target_pair "${raw}" "${source}")"
  marketing_version="${parsed%%$'\t'*}"
  build_number="${parsed#*$'\t'}"

  key="${marketing_version}:${build_number}"
  for existing in "${TARGET_KEYS[@]}"; do
    [[ "${existing}" != "${key}" ]] || fail "duplicate target: ${key}"
  done

  TARGET_KEYS+=("${key}")
  TARGET_MARKETING_VERSIONS+=("${marketing_version}")
  TARGET_BUILD_NUMBERS+=("${build_number}")
}

set_before_target() {
  local raw="$1"
  local parsed

  [[ -z "${BEFORE_TARGET}" && -z "${BEFORE_BUILD_NUMBER_ONLY}" ]] || fail "duplicate --before argument"

  if [[ "${raw}" == *:* ]]; then
    parsed="$(parse_target_pair "${raw}" "--before")"
    BEFORE_MARKETING_VERSION="${parsed%%$'\t'*}"
    BEFORE_BUILD_NUMBER="${parsed#*$'\t'}"
    BEFORE_TARGET="${BEFORE_MARKETING_VERSION}:${BEFORE_BUILD_NUMBER}"
    return
  fi

  validate_build_number "${raw}"
  BEFORE_BUILD_NUMBER_ONLY="${raw}"
}

set_before_build_number_only() {
  local raw="$1"
  local source="$2"

  [[ -z "${BEFORE_TARGET}" && -z "${BEFORE_BUILD_NUMBER_ONLY}" ]] || fail "duplicate before selector (${source})"
  validate_build_number "${raw}"
  BEFORE_BUILD_NUMBER_ONLY="${raw}"
}

has_before_selector() {
  [[ -n "${BEFORE_TARGET}" || -n "${BEFORE_BUILD_NUMBER_ONLY}" ]]
}

before_selector_label() {
  if [[ -n "${BEFORE_BUILD_NUMBER_ONLY}" ]]; then
    printf 'build_number<%s' "${BEFORE_BUILD_NUMBER_ONLY}"
    return
  fi

  printf '%s' "${BEFORE_TARGET}"
}

load_target_file() {
  local line line_no trimmed

  [[ -f "${TARGET_FILE}" ]] || fail "target file not found: ${TARGET_FILE}"

  line_no=0
  while IFS= read -r line || [[ -n "${line}" ]]; do
    line_no=$((line_no + 1))
    trimmed="$(trim_line "${line}")"

    [[ -n "${trimmed}" ]] || continue
    [[ "${trimmed}" == \#* ]] && continue

    add_target "${trimmed}" "${TARGET_FILE}:${line_no}"
  done <"${TARGET_FILE}"
}

parse_args() {
  local arg explicit_count

  while [[ $# -gt 0 ]]; do
    arg="$1"
    case "${arg}" in
      --bundle-id)
        shift
        [[ $# -gt 0 ]] || fail "missing value for --bundle-id"
        IOS_BUNDLE_ID="$1"
        ;;
      --target)
        shift
        [[ $# -gt 0 ]] || fail "missing value for --target"
        add_target "$1" "--target"
        ;;
      --target-file)
        shift
        [[ $# -gt 0 ]] || fail "missing value for --target-file"
        TARGET_FILE="$1"
        ;;
      --before)
        shift
        [[ $# -gt 0 ]] || fail "missing value for --before"
        set_before_target "$1"
        ;;
      --dry-run)
        DRY_RUN=1
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        if [[ "${arg}" =~ ^[1-9][0-9]*$ ]]; then
          set_before_build_number_only "${arg}" "positional argument"
        else
          fail "unknown argument: ${arg}"
        fi
        ;;
    esac
    shift
  done

  if [[ -n "${TARGET_FILE}" ]]; then
    load_target_file
  fi

  explicit_count="${#TARGET_KEYS[@]}"
  if has_before_selector; then
    [[ "${explicit_count}" == "0" ]] || fail "--before/<build_number> cannot be used with --target/--target-file"
    return
  fi

  [[ "${explicit_count}" -gt 0 ]] || fail "no target specified; use --target/--target-file, --before, or <build_number>"
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

  curl -sS --get "${ASC_API_BASE}${path}" \
    -H "Authorization: Bearer ${ASC_JWT_TOKEN}" \
    "$@"
}

asc_get_url() {
  local url="$1"

  curl -sS --get "${url}" \
    -H "Authorization: Bearer ${ASC_JWT_TOKEN}"
}

load_app_id() {
  local response

  response="$(asc_get "/v1/apps" \
    --data-urlencode "filter[bundleId]=${IOS_BUNDLE_ID}" \
    --data-urlencode "limit=1")"

  APP_ID="$(printf '%s' "${response}" | jq -r '.data[0].id // empty')"
  [[ -n "${APP_ID}" ]] || fail "app not found for bundle id: ${IOS_BUNDLE_ID}"
}

compare_marketing_versions() {
  local left="$1"
  local right="$2"
  local left_part right_part
  local max_parts=0
  local idx
  local -a left_parts=()
  local -a right_parts=()

  IFS='.' read -r -a left_parts <<<"${left}"
  IFS='.' read -r -a right_parts <<<"${right}"

  if (( ${#left_parts[@]} > max_parts )); then
    max_parts=${#left_parts[@]}
  fi
  if (( ${#right_parts[@]} > max_parts )); then
    max_parts=${#right_parts[@]}
  fi

  for ((idx = 0; idx < max_parts; idx++)); do
    left_part="${left_parts[idx]:-0}"
    right_part="${right_parts[idx]:-0}"

    if (( 10#${left_part} < 10#${right_part} )); then
      printf '%s' "-1"
      return
    fi
    if (( 10#${left_part} > 10#${right_part} )); then
      printf '%s' "1"
      return
    fi
  done

  printf '%s' "0"
}

is_before_target() {
  local marketing_version="$1"
  local build_number="$2"
  local cmp

  cmp="$(compare_marketing_versions "${marketing_version}" "${BEFORE_MARKETING_VERSION}")"
  if [[ "${cmp}" == "-1" ]]; then
    return 0
  fi
  if [[ "${cmp}" == "1" ]]; then
    return 1
  fi

  (( 10#${build_number} < 10#${BEFORE_BUILD_NUMBER} ))
}

is_before_build_number_only_target() {
  local build_number="$1"
  (( 10#${build_number} < 10#${BEFORE_BUILD_NUMBER_ONLY} ))
}

lookup_build_by_target() {
  local marketing_version="$1"
  local build_number="$2"
  local response count

  response="$(asc_get "/v1/builds" \
    --data-urlencode "filter[app]=${APP_ID}" \
    --data-urlencode "filter[version]=${build_number}" \
    --data-urlencode "filter[preReleaseVersion.version]=${marketing_version}" \
    --data-urlencode "sort=-uploadedDate" \
    --data-urlencode "limit=2" \
    --data-urlencode "fields[builds]=version,expired,processingState,uploadedDate")"

  ensure_asc_data_array "${response}" "lookup target ${marketing_version}:${build_number}"
  count="$(printf '%s' "${response}" | jq -r '.data | length')"
  if [[ "${count}" == "0" ]]; then
    fail "build not found for target ${marketing_version}:${build_number}"
  fi
  if [[ "${count}" != "1" ]]; then
    fail "target ${marketing_version}:${build_number} matched ${count} builds; aborting to avoid ambiguity"
  fi

  LOOKUP_BUILD_ID="$(printf '%s' "${response}" | jq -r '.data[0].id // empty')"
  LOOKUP_BUILD_EXPIRED="$(printf '%s' "${response}" | jq -r '.data[0].attributes.expired // false')"
  LOOKUP_BUILD_PROCESSING_STATE="$(printf '%s' "${response}" | jq -r '.data[0].attributes.processingState // empty')"
  LOOKUP_BUILD_UPLOADED_DATE="$(printf '%s' "${response}" | jq -r '.data[0].attributes.uploadedDate // empty')"

  [[ -n "${LOOKUP_BUILD_ID}" ]] || fail "missing build id for target ${marketing_version}:${build_number}"
}

collect_before_builds_from_response() {
  local response="$1"
  local parsed_rows
  local row
  local build_id build_number marketing_version expired state uploaded_date
  local before_label

  before_label="$(before_selector_label)"
  ensure_asc_data_array "${response}" "list builds before ${before_label}"
  parsed_rows="$(
    printf '%s' "${response}" | jq -r '
      (.included // [] | map(select(.type == "preReleaseVersions")) | map({key: .id, value: (.attributes.version // "")}) | from_entries) as $pre
      | .data[]
      | {
          id: (.id // ""),
          build_number: (.attributes.version // ""),
          marketing_version: ($pre[(.relationships.preReleaseVersion.data.id // "")] // ""),
          expired: ((.attributes.expired // false) | tostring),
          state: (.attributes.processingState // ""),
          uploaded_date: (.attributes.uploadedDate // "")
        }
      | @json
    '
  )" || fail "list builds before ${before_label}: failed to parse App Store Connect response"

  while IFS= read -r row; do
    [[ -n "${row}" ]] || continue
    build_id="$(printf '%s' "${row}" | jq -r '.id // empty')"
    build_number="$(printf '%s' "${row}" | jq -r '.build_number // empty')"
    marketing_version="$(printf '%s' "${row}" | jq -r '.marketing_version // empty')"
    expired="$(printf '%s' "${row}" | jq -r '.expired // "false"')"
    state="$(printf '%s' "${row}" | jq -r '.state // empty')"
    uploaded_date="$(printf '%s' "${row}" | jq -r '.uploaded_date // empty')"

    [[ -n "${build_id}" ]] || fail "build id is empty in ASC response"
    [[ -n "${build_number}" ]] || fail "build number is empty for build id=${build_id}"
    validate_build_number "${build_number}"

    if [[ -n "${BEFORE_BUILD_NUMBER_ONLY}" ]]; then
      if is_before_build_number_only_target "${build_number}"; then
        BEFORE_BUILD_IDS+=("${build_id}")
        BEFORE_BUILD_MARKETING_VERSIONS+=("${marketing_version}")
        BEFORE_BUILD_NUMBERS+=("${build_number}")
        BEFORE_BUILD_EXPIRED_FLAGS+=("${expired}")
        BEFORE_BUILD_STATES+=("${state}")
        BEFORE_BUILD_UPLOADED_DATES+=("${uploaded_date}")
      fi
      continue
    fi

    if [[ -z "${marketing_version}" ]]; then
      log "skip build_id=${build_id} build_number=${build_number} because marketing version is unavailable"
      continue
    fi
    validate_marketing_version "${marketing_version}"

    if is_before_target "${marketing_version}" "${build_number}"; then
      BEFORE_BUILD_IDS+=("${build_id}")
      BEFORE_BUILD_MARKETING_VERSIONS+=("${marketing_version}")
      BEFORE_BUILD_NUMBERS+=("${build_number}")
      BEFORE_BUILD_EXPIRED_FLAGS+=("${expired}")
      BEFORE_BUILD_STATES+=("${state}")
      BEFORE_BUILD_UPLOADED_DATES+=("${uploaded_date}")
    fi
  done <<<"${parsed_rows}"
}

resolve_before_targets() {
  local response next_url

  ASC_JWT_TOKEN="$(generate_jwt)"
  response="$(asc_get "/v1/builds" \
    --data-urlencode "filter[app]=${APP_ID}" \
    --data-urlencode "sort=-uploadedDate" \
    --data-urlencode "limit=200" \
    --data-urlencode "include=preReleaseVersion" \
    --data-urlencode "fields[builds]=version,expired,processingState,uploadedDate" \
    --data-urlencode "fields[preReleaseVersions]=version")"
  collect_before_builds_from_response "${response}"

  next_url="$(printf '%s' "${response}" | jq -r '.links.next // empty')"
  while [[ -n "${next_url}" ]]; do
    ASC_JWT_TOKEN="$(generate_jwt)"
    response="$(asc_get_url "${next_url}")"
    collect_before_builds_from_response "${response}"
    next_url="$(printf '%s' "${response}" | jq -r '.links.next // empty')"
  done
}

expire_build() {
  local build_id="$1"
  local payload response_file http_code

  payload="$(jq -n --arg build_id "${build_id}" '{data:{type:"builds",id:$build_id,attributes:{expired:true}}}')"
  response_file="$(mktemp)"

  http_code="$(
    curl -sS \
      -X PATCH "${ASC_API_BASE}/v1/builds/${build_id}" \
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

  log "App Store Connect response: $(cat "${response_file}")"
  rm -f "${response_file}"
  fail "failed to expire build id=${build_id} (http=${http_code})"
}

process_explicit_targets() {
  local idx marketing_version build_number
  local expired_count already_expired_count total_count

  total_count="${#TARGET_KEYS[@]}"
  expired_count=0
  already_expired_count=0

  log "mode=explicit target app=${IOS_BUNDLE_ID}, total_targets=${total_count}, dry_run=${DRY_RUN}"

  for ((idx = 0; idx < total_count; idx++)); do
    marketing_version="${TARGET_MARKETING_VERSIONS[idx]}"
    build_number="${TARGET_BUILD_NUMBERS[idx]}"

    ASC_JWT_TOKEN="$(generate_jwt)"
    lookup_build_by_target "${marketing_version}" "${build_number}"

    log "matched target=${marketing_version}:${build_number} build_id=${LOOKUP_BUILD_ID} state=${LOOKUP_BUILD_PROCESSING_STATE:-unknown} uploaded=${LOOKUP_BUILD_UPLOADED_DATE:-unknown} expired=${LOOKUP_BUILD_EXPIRED}"

    if [[ "${LOOKUP_BUILD_EXPIRED}" == "true" ]]; then
      already_expired_count=$((already_expired_count + 1))
      continue
    fi

    if [[ "${DRY_RUN}" == "1" ]]; then
      log "dry-run: skip patch target=${marketing_version}:${build_number}"
      continue
    fi

    ASC_JWT_TOKEN="$(generate_jwt)"
    expire_build "${LOOKUP_BUILD_ID}"
    expired_count=$((expired_count + 1))
    log "expired target=${marketing_version}:${build_number} build_id=${LOOKUP_BUILD_ID}"
  done

  if [[ "${DRY_RUN}" == "1" ]]; then
    log "dry-run completed: inspected=${total_count}, already_expired=${already_expired_count}"
  else
    log "completed: expired=${expired_count}, already_expired=${already_expired_count}, total=${total_count}"
  fi
}

process_before_target() {
  local idx build_id marketing_version build_number expired state uploaded_date
  local total_count expired_count already_expired_count before_label target_label

  resolve_before_targets
  total_count="${#BEFORE_BUILD_IDS[@]}"
  expired_count=0
  already_expired_count=0
  before_label="$(before_selector_label)"

  log "mode=before target app=${IOS_BUNDLE_ID}, before=${before_label}, matched_targets=${total_count}, dry_run=${DRY_RUN}"

  if [[ "${total_count}" == "0" ]]; then
    log "no iOS builds found before ${before_label}"
    return
  fi

  for ((idx = 0; idx < total_count; idx++)); do
    build_id="${BEFORE_BUILD_IDS[idx]}"
    marketing_version="${BEFORE_BUILD_MARKETING_VERSIONS[idx]}"
    build_number="${BEFORE_BUILD_NUMBERS[idx]}"
    expired="${BEFORE_BUILD_EXPIRED_FLAGS[idx]}"
    state="${BEFORE_BUILD_STATES[idx]}"
    uploaded_date="${BEFORE_BUILD_UPLOADED_DATES[idx]}"
    target_label="${build_number}"
    if [[ -n "${marketing_version}" ]]; then
      target_label="${marketing_version}:${build_number}"
    fi

    log "matched target=${target_label} build_id=${build_id} state=${state:-unknown} uploaded=${uploaded_date:-unknown} expired=${expired}"

    if [[ "${expired}" == "true" ]]; then
      already_expired_count=$((already_expired_count + 1))
      continue
    fi

    if [[ "${DRY_RUN}" == "1" ]]; then
      log "dry-run: skip patch target=${target_label}"
      continue
    fi

    ASC_JWT_TOKEN="$(generate_jwt)"
    expire_build "${build_id}"
    expired_count=$((expired_count + 1))
    log "expired target=${target_label} build_id=${build_id}"
  done

  if [[ "${DRY_RUN}" == "1" ]]; then
    log "dry-run completed: matched=${total_count}, already_expired=${already_expired_count}"
  else
    log "completed: expired=${expired_count}, already_expired=${already_expired_count}, matched=${total_count}"
  fi
}

main() {
  parse_args "$@"

  require_cmd xcrun
  require_cmd jq
  require_cmd curl
  require_env APPSTORE_API_KEY_ID
  require_env APPSTORE_API_ISSUER_ID

  ASC_JWT_TOKEN="$(generate_jwt)"
  load_app_id

  if has_before_selector; then
    process_before_target
    return
  fi

  process_explicit_targets
}

main "$@"
