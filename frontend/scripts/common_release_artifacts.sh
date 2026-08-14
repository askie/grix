#!/usr/bin/env bash
set -euo pipefail

require_git_cmd_for_release_artifacts() {
  if ! command -v git >/dev/null 2>&1; then
    echo "[release-artifacts] missing required command: git" >&2
    exit 1
  fi
}

git_common_repo_root() {
  local common_git_dir

  require_git_cmd_for_release_artifacts
  if ! common_git_dir="$(git -C "${FRONTEND_ROOT}" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"; then
    echo "[release-artifacts] failed to resolve git common dir from ${FRONTEND_ROOT}" >&2
    exit 1
  fi

  (
    cd "${common_git_dir}/.."
    pwd
  )
}

release_artifacts_root() {
  if [[ -n "${AIBOT_RELEASE_ARTIFACTS_DIR:-}" ]]; then
    echo "${AIBOT_RELEASE_ARTIFACTS_DIR}"
    return
  fi

  echo "$(git_common_repo_root)/release_artifacts"
}

android_release_artifact_dir() {
  local mode="$1"

  case "${mode}" in
    apk)
      echo "$(release_artifacts_root)/android/apk"
      ;;
    appbundle|aab)
      echo "$(release_artifacts_root)/android/aab"
      ;;
    *)
      echo "[release-artifacts] unsupported android artifact mode: ${mode}" >&2
      exit 1
      ;;
  esac
}

archive_release_artifact() {
  local source_path="$1"
  local target_dir="$2"
  local target_path

  [[ -f "${source_path}" ]] || {
    echo "[release-artifacts] build output not found: ${source_path}" >&2
    exit 1
  }

  mkdir -p "${target_dir}"
  target_path="${target_dir}/$(basename "${source_path}")"
  cp -f "${source_path}" "${target_path}"
  echo "${target_path}"
}
