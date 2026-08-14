#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"
# shellcheck source=./common_android_env.sh
. "${SCRIPT_DIR}/common_android_env.sh"
# shellcheck source=./common_release_artifacts.sh
. "${SCRIPT_DIR}/common_release_artifacts.sh"

require_unified_release_call "android-push" "./scripts/release.sh client publish android（公开包已迁移到 CI）"

MODE="${1:-apk}"
BUILD_MODE="${BUILD_MODE:-release}"

setup_android_java_home
load_push_env
require_frontend_base_env
require_env AIBOT_ANDROID_FIREBASE_API_KEY
require_env AIBOT_ANDROID_FIREBASE_APP_ID
require_env AIBOT_ANDROID_FIREBASE_PROJECT_ID
require_env AIBOT_ANDROID_FIREBASE_SENDER_ID
require_env AIBOT_PUSH_JPUSH_APP_KEY
# 华为 App ID 缺失不会编译失败，只会让华为设备静默降级到极光，必须硬拦。
require_env AIBOT_ANDROID_HUAWEI_APP_ID
export AIBOT_PUSH_JPUSH_APP_KEY
export AIBOT_ANDROID_HUAWEI_APP_ID
build_flutter_define_args
build_flutter_release_args

case "${MODE}" in
  apk)
    TARGET=(build apk)
    ;;
  appbundle|aab)
    TARGET=(build appbundle)
    ;;
  run)
    TARGET=(run)
    ;;
  *)
    echo "usage: $0 [apk|appbundle|aab|run]" >&2
    exit 1
    ;;
esac

android_build_output_path() {
  local mode="$1"
  local build_mode="$2"

  case "${mode}" in
    apk)
      echo "${FRONTEND_ROOT}/build/app/outputs/flutter-apk/app-arm64-v8a-${build_mode}.apk"
      ;;
    appbundle|aab)
      echo "${FRONTEND_ROOT}/build/app/outputs/bundle/${build_mode}/app-${build_mode}.aab"
      ;;
    *)
      echo "[android-push] unsupported artifact mode: ${mode}" >&2
      exit 1
      ;;
  esac
}

archive_android_artifact() {
  local mode="$1"
  local build_mode="$2"
  local source_artifact_path
  local target_artifact_dir
  local archived_artifact_path

  source_artifact_path="$(android_build_output_path "${mode}" "${build_mode}")"
  target_artifact_dir="$(android_release_artifact_dir "${mode}")"
  archived_artifact_path="$(archive_release_artifact "${source_artifact_path}" "${target_artifact_dir}")"
  echo "[android-push] archived artifact: ${archived_artifact_path}"
}

cd "${FRONTEND_ROOT}"
flutter pub get

CMD=(flutter "${TARGET[@]}")
if [ "${MODE}" != "run" ]; then
  CMD+=("--${BUILD_MODE}")
fi
if [ "${MODE}" = "apk" ]; then
  CMD+=("--split-per-abi")
fi
CMD+=("${FLUTTER_RELEASE_ARGS[@]}")
CMD+=("${FLUTTER_DEFINE_ARGS[@]}")

echo "[android-push] running: ${CMD[*]}"
"${CMD[@]}"

if [ "${MODE}" != "run" ]; then
  archive_android_artifact "${MODE}" "${BUILD_MODE}"
fi
