#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"

require_unified_release_call "ios-push" "./scripts/release.sh mobile-ios-ipa"

MODE="${1:-ipa}"
BUILD_MODE="${BUILD_MODE:-release}"

case "${MODE}" in
  ipa)
    TARGET=(build ipa)
    ;;
  run)
    TARGET=(run)
    ;;
  *)
    echo "usage: $0 [ipa|run]" >&2
    exit 1
    ;;
esac

clean_ios_ipa_artifacts() {
  local stale_dirs=(
    "${FRONTEND_ROOT}/build/ios/ipa"
    "${FRONTEND_ROOT}/build/ios/archive"
  )
  local path
  for path in "${stale_dirs[@]}"; do
    if [ -e "${path}" ]; then
      echo "[ios-push] removing stale artifact: ${path}"
      rm -rf "${path}"
    fi
  done
}

configure_stable_xcode_cache() {
  local xcrun_dir="${FRONTEND_ROOT}/build/ios/xcode-tooling"
  local xcrun_wrapper="${SCRIPT_DIR}/xcrun_with_stable_derived_data.sh"

  export AIBOT_IOS_DERIVED_DATA_PATH="${AIBOT_IOS_DERIVED_DATA_PATH:-${HOME}/Library/Caches/aibot/frontend-ios-derived-data}"
  mkdir -p "${AIBOT_IOS_DERIVED_DATA_PATH}" "${xcrun_dir}"
  ln -sfn "${xcrun_wrapper}" "${xcrun_dir}/xcrun"
  export PATH="${xcrun_dir}:${PATH}"
  echo "[ios-push] xcode DerivedData cache: ${AIBOT_IOS_DERIVED_DATA_PATH}"
}

load_push_env
require_frontend_base_env
if [ "${MODE}" = "ipa" ] && [ "${BUILD_MODE}" = "release" ]; then
  require_ios_release_env
  IOS_TEAM_ID="RB6MGXAF36" require_ios_release_signing_assets
fi
build_flutter_define_args
build_flutter_release_args

cd "${FRONTEND_ROOT}"
flutter pub get

if [ "${MODE}" = "ipa" ]; then
  clean_ios_ipa_artifacts
  configure_stable_xcode_cache
fi

CMD=(flutter "${TARGET[@]}")
if [ "${MODE}" != "run" ]; then
  CMD+=("--${BUILD_MODE}")
fi
CMD+=("${FLUTTER_DEFINE_ARGS[@]}")
CMD+=("${FLUTTER_RELEASE_ARGS[@]}")

echo "[ios-push] running: ${CMD[*]}"
"${CMD[@]}"
