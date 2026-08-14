#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
  echo "[android-setup] this script only supports macOS" >&2
  exit 1
fi

if ! command -v brew >/dev/null 2>&1; then
  echo "[android-setup] Homebrew is required: https://brew.sh" >&2
  exit 1
fi

if ! command -v flutter >/dev/null 2>&1; then
  echo "[android-setup] Flutter is required and must be on PATH" >&2
  exit 1
fi

ANDROID_SDK_ROOT_DEFAULT="/opt/homebrew/share/android-commandlinetools"
ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-${ANDROID_SDK_ROOT_DEFAULT}}"
ANDROID_API_LEVEL="${ANDROID_API_LEVEL:-35}"
ANDROID_BUILD_TOOLS="${ANDROID_BUILD_TOOLS:-35.0.0}"

echo "[android-setup] installing JDK 17"
brew install --cask temurin@17

JAVA_HOME_CANDIDATE="/Library/Java/JavaVirtualMachines/temurin-17.jdk/Contents/Home"
if [ -d "${JAVA_HOME_CANDIDATE}" ]; then
  export JAVA_HOME="${JAVA_HOME_CANDIDATE}"
fi

echo "[android-setup] installing Android command line tools"
brew install --cask android-commandlinetools
brew install --cask android-platform-tools

export ANDROID_SDK_ROOT
export ANDROID_HOME="${ANDROID_SDK_ROOT}"
export PATH="${ANDROID_SDK_ROOT}/cmdline-tools/latest/bin:${PATH}"

echo "[android-setup] configuring Flutter android-sdk=${ANDROID_SDK_ROOT}"
flutter config --enable-android --android-sdk "${ANDROID_SDK_ROOT}"

LOCAL_PROPERTIES_FILE="${FRONTEND_ROOT}/android/local.properties"
if [ -f "${LOCAL_PROPERTIES_FILE}" ]; then
  if grep -q '^sdk.dir=' "${LOCAL_PROPERTIES_FILE}"; then
    perl -0pi -e 's#^sdk\.dir=.*$#sdk.dir=/opt/homebrew/share/android-commandlinetools#m' "${LOCAL_PROPERTIES_FILE}"
  else
    printf '\nsdk.dir=%s\n' "${ANDROID_SDK_ROOT}" >> "${LOCAL_PROPERTIES_FILE}"
  fi
else
  printf 'sdk.dir=%s\n' "${ANDROID_SDK_ROOT}" > "${LOCAL_PROPERTIES_FILE}"
fi

if ! command -v sdkmanager >/dev/null 2>&1; then
  echo "[android-setup] sdkmanager not found after installing android-commandlinetools" >&2
  exit 1
fi

yes | sdkmanager --sdk_root="${ANDROID_SDK_ROOT}" --licenses >/dev/null

sdkmanager --sdk_root="${ANDROID_SDK_ROOT}" \
  "platform-tools" \
  "platforms;android-${ANDROID_API_LEVEL}" \
  "build-tools;${ANDROID_BUILD_TOOLS}"

cat <<EOF
[android-setup] done
JAVA_HOME=${JAVA_HOME:-}
ANDROID_SDK_ROOT=${ANDROID_SDK_ROOT}

Add these lines to your shell profile if you want them persisted:
  export JAVA_HOME="${JAVA_HOME_CANDIDATE}"
  export ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT}"
  export ANDROID_HOME="${ANDROID_SDK_ROOT}"
  export PATH="\$ANDROID_SDK_ROOT/cmdline-tools/latest/bin:\$PATH"

Validation:
  java -version
  flutter doctor -v
  cd /path/to/grix/frontend && ./scripts/android_push_build.sh apk
EOF
