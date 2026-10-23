#!/usr/bin/env bash
# Upload an Android App Bundle to Google Play (default: internal track).
#
# Fast path (AAB already local):
#   frontend/scripts/android_upload_play.sh --aab /path/to/app-release.aab
#
# From a finished CI run artifact (uses local proxy when present):
#   frontend/scripts/android_upload_play.sh --from-ci-run 39905033233
#
# Env:
#   PLAY_SERVICE_ACCOUNT_JSON  service-account JSON (default: ~/.config/grix/secrets/play-upload.json)
#   https_proxy / HTTP_PROXY   used for GitHub artifact download (default tries 127.0.0.1:20001)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
PY="${SCRIPT_DIR}/android_upload_play.py"
CACHE_ROOT="${HOME}/.grix-release-cache/play-upload"

log() { echo "[android-upload-play] $*" >&2; }
fail() { echo "[android-upload-play] ERROR: $*" >&2; exit 1; }

AAB=""
CI_RUN=""
TRACK="internal"
STATUS="completed"
NAME=""
SA=""
PACKAGE="pub.dhf.grix"

usage() {
  sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --aab) AAB="${2:-}"; shift 2 ;;
    --from-ci-run) CI_RUN="${2:-}"; shift 2 ;;
    --track) TRACK="${2:-}"; shift 2 ;;
    --status) STATUS="${2:-}"; shift 2 ;;
    --name) NAME="${2:-}"; shift 2 ;;
    --sa) SA="${2:-}"; shift 2 ;;
    --package) PACKAGE="${2:-}"; shift 2 ;;
    -h|--help) usage ;;
    *) fail "unknown arg: $1 (see --help)" ;;
  esac
done

[[ -f "${PY}" ]] || fail "missing ${PY}"
command -v python3 >/dev/null 2>&1 || fail "python3 required"

# Prefer the release-machine proxy for GitHub artifact downloads; ignore if down.
ensure_github_proxy() {
  if [[ -n "${https_proxy:-}${HTTPS_PROXY:-}${http_proxy:-}${HTTP_PROXY:-}" ]]; then
    return 0
  fi
  if curl -fsS -m 1 "http://127.0.0.1:20001/" >/dev/null 2>&1 \
    || curl -fsS -m 1 -x "http://127.0.0.1:20001" "https://api.github.com/" >/dev/null 2>&1; then
    export https_proxy="http://127.0.0.1:20001"
    export http_proxy="http://127.0.0.1:20001"
    export HTTPS_PROXY="${https_proxy}"
    export HTTP_PROXY="${http_proxy}"
    log "using local proxy ${https_proxy}"
  fi
}

download_ci_aab() {
  local run_id="$1"
  local out_dir art_id token zip_path
  command -v gh >/dev/null 2>&1 || fail "gh required for --from-ci-run"
  command -v curl >/dev/null 2>&1 || fail "curl required for --from-ci-run"
  command -v unzip >/dev/null 2>&1 || fail "unzip required for --from-ci-run"

  ensure_github_proxy
  out_dir="${CACHE_ROOT}/${run_id}"
  mkdir -p "${out_dir}"
  zip_path="${out_dir}/android-aab.zip"

  log "resolving android-aab artifact for run ${run_id}"
  art_id="$(
    gh api "repos/askie/grix/actions/runs/${run_id}/artifacts" \
      --jq '.artifacts[] | select(.name=="android-aab") | .id' \
      | head -n 1
  )"
  [[ -n "${art_id}" ]] || fail "android-aab artifact not found on run ${run_id}"

  token="$(gh auth token)"
  [[ -n "${token}" ]] || fail "gh auth token empty; run gh auth login"

  log "downloading artifact ${art_id} -> ${zip_path}"
  curl -fL --retry 3 \
    -H "Authorization: Bearer ${token}" \
    -H "Accept: application/vnd.github+json" \
    -o "${zip_path}" \
    "https://api.github.com/repos/askie/grix/actions/artifacts/${art_id}/zip"

  unzip -o -q "${zip_path}" -d "${out_dir}"
  AAB="$(find "${out_dir}" -maxdepth 1 -name '*.aab' -print -quit)"
  [[ -n "${AAB}" && -f "${AAB}" ]] || fail "no .aab inside artifact zip"
  log "aab ready: ${AAB} ($(du -h "${AAB}" | awk '{print $1}'))"
}

if [[ -n "${CI_RUN}" ]]; then
  download_ci_aab "${CI_RUN}"
fi

[[ -n "${AAB}" ]] || fail "provide --aab PATH or --from-ci-run RUN_ID"
[[ -f "${AAB}" ]] || fail "AAB not found: ${AAB}"

args=(--aab "${AAB}" --package "${PACKAGE}" --track "${TRACK}" --status "${STATUS}")
[[ -n "${NAME}" ]] && args+=(--name "${NAME}")
[[ -n "${SA}" ]] && args+=(--sa "${SA}")

log "uploading to Play track=${TRACK} status=${STATUS}"
python3 "${PY}" "${args[@]}"
log "done"
