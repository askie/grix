#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"
# shellcheck source=./common_android_env.sh
. "${SCRIPT_DIR}/common_android_env.sh"

setup_android_java_home

KEYSTORE_PATH="${KEYSTORE_PATH:-${FRONTEND_ROOT}/android/app/upload-keystore.jks}"
KEY_ALIAS="${KEY_ALIAS:-upload}"
KEY_VALIDITY_DAYS="${KEY_VALIDITY_DAYS:-10000}"
KEYSTORE_STOREPASS="${KEYSTORE_STOREPASS:-}"
KEYSTORE_KEYPASS="${KEYSTORE_KEYPASS:-}"
KEYSTORE_DNAME="${KEYSTORE_DNAME:-}"
ANDROID_ROOT="${FRONTEND_ROOT}/android"

if [ -e "${KEYSTORE_PATH}" ]; then
  echo "[android-keystore] file already exists: ${KEYSTORE_PATH}" >&2
  exit 1
fi

mkdir -p "$(dirname "${KEYSTORE_PATH}")"

echo "[android-keystore] using JAVA_HOME=${JAVA_HOME}"
echo "[android-keystore] creating ${KEYSTORE_PATH}"

CMD=("${JAVA_HOME}/bin/keytool" -genkeypair \
  -v \
  -keystore "${KEYSTORE_PATH}" \
  -alias "${KEY_ALIAS}" \
  -keyalg RSA \
  -keysize 2048 \
  -validity "${KEY_VALIDITY_DAYS}")

if [ -n "${KEYSTORE_STOREPASS}" ]; then
  CMD+=(-storepass "${KEYSTORE_STOREPASS}")
fi

if [ -n "${KEYSTORE_KEYPASS}" ]; then
  CMD+=(-keypass "${KEYSTORE_KEYPASS}")
fi

if [ -n "${KEYSTORE_DNAME}" ]; then
  CMD+=(-dname "${KEYSTORE_DNAME}")
fi

"${CMD[@]}"

STORE_FILE_VALUE="${KEYSTORE_PATH}"
case "${KEYSTORE_PATH}" in
  "${ANDROID_ROOT}/"*)
    STORE_FILE_VALUE="${KEYSTORE_PATH#${ANDROID_ROOT}/}"
    ;;
esac

cat <<EOF
[android-keystore] done

Create frontend/android/key.properties with:
  storePassword=<your-store-password>
  keyPassword=<your-key-password>
  keyAlias=${KEY_ALIAS}
  storeFile=${STORE_FILE_VALUE}
EOF
