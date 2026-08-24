#!/usr/bin/env bash
set -euo pipefail

# admin 端 iOS 构建脚本
# 仅由 grix-ops 的前端发布入口调用。

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ADMIN_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROOT_DIR="$(cd "${ADMIN_ROOT}/.." && pwd)"

GRIX_OPS_DIR="${GRIX_OPS_DIR:-$(cd "${ROOT_DIR}/.." && pwd)/grix-ops}"
COMMON_RELEASE_ENV="${GRIX_OPS_DIR}/scripts/common_release_env.sh"
if [[ ! -f "${COMMON_RELEASE_ENV}" ]]; then
  echo "[admin-ios] ERROR: missing private release environment: ${COMMON_RELEASE_ENV}" >&2
  echo "[admin-ios] Set GRIX_OPS_DIR to the private grix-ops checkout." >&2
  exit 1
fi
# shellcheck source=/dev/null
. "${COMMON_RELEASE_ENV}"

# 限制只能通过 grix-ops 前端发布入口调用；AIBOT_* 保留旧入口兼容。
if ! { [ "${GRIX_RELEASE_UNIFIED_CALL:-0}" = "1" ] && \
       [ "${GRIX_RELEASE_ENTRYPOINT:-}" = "frontend" ]; } && \
   [ "${AIBOT_RELEASE_UNIFIED_CALL:-0}" != "1" ]; then
  echo "[admin-ios] ERROR: 请通过 grix-ops/scripts/release-frontend.sh admin-ios 调用" >&2
  exit 1
fi

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
    echo "用法: $0 [ipa|run]" >&2
    exit 1
    ;;
esac

clean_ios_ipa_artifacts() {
  local stale_dirs=(
    "${ADMIN_ROOT}/build/ios/ipa"
    "${ADMIN_ROOT}/build/ios/archive"
    "${ADMIN_ROOT}/build/ios/iphoneos"
  )
  for path in "${stale_dirs[@]}"; do
    if [ -e "${path}" ]; then
      echo "[admin-ios] 清理旧产物: ${path}"
      rm -rf "${path}"
    fi
  done
}

cd "${ADMIN_ROOT}"
flutter pub get

if [ "${MODE}" = "ipa" ]; then
  clean_ios_ipa_artifacts
fi

# 不注入 ADMIN_API_BASE_URL：区域由运行时 AdminRegionStore 决定（CN/Global 均可切换）。
# 仅在外部显式传入时透传，用于 CI / 特殊部署覆盖。
CMD=(flutter "${TARGET[@]}")
if [ "${MODE}" != "run" ]; then
  CMD+=("--${BUILD_MODE}")
fi
if [ -n "${ADMIN_API_BASE_URL:-}" ]; then
  CMD+=("--dart-define=ADMIN_API_BASE_URL=${ADMIN_API_BASE_URL}")
fi

echo "[admin-ios] 执行: ${CMD[*]}"
if [ "${MODE}" = "ipa" ]; then
  # Flutter's IPA export can fail when command-line ASC API credentials do not
  # match Xcode's signed-in account. Archive first, then let Xcode export using
  # the local account/profile state.
  ARCHIVE_CMD=(flutter build ios "--${BUILD_MODE}" "--no-codesign")
  if [ -n "${ADMIN_API_BASE_URL:-}" ]; then
    ARCHIVE_CMD+=("--dart-define=ADMIN_API_BASE_URL=${ADMIN_API_BASE_URL}")
  fi
  echo "[admin-ios] 执行: ${ARCHIVE_CMD[*]}"
  "${ARCHIVE_CMD[@]}"
  xcodebuild -workspace ios/Runner.xcworkspace \
    -scheme Runner \
    -configuration Release \
    -destination generic/platform=iOS \
    -archivePath build/ios/archive/Runner.xcarchive \
    archive \
    DEVELOPMENT_TEAM=RB6MGXAF36
  xcodebuild -exportArchive \
    -archivePath build/ios/archive/Runner.xcarchive \
    -exportPath build/ios/ipa \
    -exportOptionsPlist ios/ExportOptions.plist \
    -allowProvisioningUpdates
else
  "${CMD[@]}"
fi
