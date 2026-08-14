#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
  echo "[ios-setup] this script only supports macOS" >&2
  exit 1
fi

if ! command -v xcodebuild >/dev/null 2>&1; then
  echo "[ios-setup] Xcode command line tools are required" >&2
  exit 1
fi

PLATFORM="${1:-iOS}"
EXPORT_PATH="${EXPORT_PATH:-${HOME}/Downloads}"

echo "[ios-setup] selecting default Xcode"
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer

echo "[ios-setup] running first launch setup"
sudo xcodebuild -runFirstLaunch

echo "[ios-setup] downloading ${PLATFORM} simulator runtime to ${EXPORT_PATH}"
xcodebuild -downloadPlatform "${PLATFORM}" -exportPath "${EXPORT_PATH}"

cat <<EOF
[ios-setup] download finished

If the runtime was not auto-imported, run:
  xcodebuild -importPlatform "<downloaded .dmg path>"

Validation:
  xcrun simctl list runtimes
  cd /path/to/grix/frontend && ./scripts/ios_push_build.sh run
EOF
