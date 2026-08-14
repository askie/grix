#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common_push_env.sh
. "${SCRIPT_DIR}/common_push_env.sh"
# shellcheck source=./common_android_env.sh
. "${SCRIPT_DIR}/common_android_env.sh"

require_unified_release_call "preflight" "./scripts/release.sh mobile-preflight"

setup_android_java_home

ENV_FILE="${PUSH_ENV_FILE:-$(default_push_env_file)}"
# 编进 APK 的客户端公开标识独立存放（.env.push.android.local，唯一定义处），
# 由 .env.push.local 以 `.` 引用。本脚本用 awk 逐行解析而非 shell source，
# 看不到嵌套引用，故需要显式回退到该文件查找。可用 PUSH_ANDROID_ENV_FILE 覆盖
# （与 release-public.yml 的变量名一致）。
ANDROID_ENV_FILE="${PUSH_ANDROID_ENV_FILE:-$(release_resolve_repo_local_file "${FRONTEND_ROOT}/.." "frontend/.env.push.android.local")}"
ANDROID_ROOT="${FRONTEND_ROOT}/android"
KEY_PROPS_FILE="$(release_resolve_repo_local_file "${FRONTEND_ROOT}/.." "frontend/android/key.properties")"
KEYSTORE_PATH=""
KEY_ALIAS=""
KEYSTORE_PASSWORD=""
KEY_PASSWORD=""
BUILD_MODE="${BUILD_MODE:-release}"
PREFLIGHT_FAILED=0

status_line() {
  printf '[preflight] %-32s %s\n' "$1" "$2"
}

read_key_prop() {
  local key="$1"
  awk -F= -v target="${key}" '$1 == target { sub(/^[^=]*=/, ""); print; exit }' "${KEY_PROPS_FILE}"
}

resolve_signing_file() {
  local raw_path="$1"

  if [ -z "${raw_path}" ]; then
    return 0
  fi

  case "${raw_path}" in
    /*)
      printf '%s\n' "${raw_path}"
      ;;
    *)
      printf '%s\n' "$(cd "$(dirname "${KEY_PROPS_FILE}")" && pwd)/${raw_path}"
      ;;
  esac
}

check_env_var() {
  local name="$1"
  local optional="${2:-0}"
  if [ ! -f "${ENV_FILE}" ]; then
    if [ "${optional}" = "1" ]; then
      status_line "${name}" "missing (.env.push.local not found, optional)"
      return 0
    else
      status_line "${name}" "missing (.env.push.local not found)"
      return 1
    fi
  fi

  local value
  value="$(awk -F= -v target="${name}" '$1 == target { sub(/^[^=]*=/, ""); print; exit }' "${ENV_FILE}")"
  if [ -z "${value}" ] && [ -f "${ANDROID_ENV_FILE}" ]; then
    value="$(awk -F= -v target="${name}" '$1 == target { sub(/^[^=]*=/, ""); print; exit }' "${ANDROID_ENV_FILE}")"
  fi
  if [ -n "${value}" ]; then
    status_line "${name}" "set"
    return 0
  elif [ "${optional}" = "1" ]; then
    status_line "${name}" "missing (optional)"
    return 0
  else
    status_line "${name}" "missing"
    return 1
  fi
}

echo "[preflight] Android release preflight"
status_line "JAVA_HOME" "${JAVA_HOME}"

if [ -f "${KEY_PROPS_FILE}" ]; then
  status_line "android/key.properties" "present"
  KEYSTORE_PATH="$(resolve_signing_file "$(read_key_prop storeFile)")"
  KEY_ALIAS="$(read_key_prop keyAlias)"
  KEYSTORE_PASSWORD="$(read_key_prop storePassword)"
  KEY_PASSWORD="$(read_key_prop keyPassword)"
else
  status_line "android/key.properties" "missing"
fi

if [ -n "${KEYSTORE_PATH}" ] && [ -f "${KEYSTORE_PATH}" ]; then
  status_line "upload keystore" "present (${KEYSTORE_PATH})"
else
  status_line "upload keystore" "missing"
fi

if [ "${BUILD_MODE}" = "release" ]; then
  if [ ! -f "${KEY_PROPS_FILE}" ]; then
    status_line "android release signing" "missing key.properties"
    PREFLIGHT_FAILED=1
  elif [ -z "${KEYSTORE_PATH}" ] || [ ! -f "${KEYSTORE_PATH}" ]; then
    status_line "android release signing" "missing upload keystore"
    PREFLIGHT_FAILED=1
  elif [ -z "${KEY_ALIAS}" ] || [ -z "${KEYSTORE_PASSWORD}" ] || [ -z "${KEY_PASSWORD}" ]; then
    status_line "android release signing" "missing keyAlias/keyPassword/storePassword"
    PREFLIGHT_FAILED=1
  else
    status_line "android release signing" "ready"
  fi
fi

if [ -n "${KEYSTORE_PATH}" ] && [ -f "${KEYSTORE_PATH}" ] && [ -n "${KEY_ALIAS}" ] && [ -n "${KEYSTORE_PASSWORD}" ]; then
  SHA256_LINE="$("${JAVA_HOME}/bin/keytool" -list -keystore "${KEYSTORE_PATH}" -storepass "${KEYSTORE_PASSWORD}" -alias "${KEY_ALIAS}" 2>/dev/null | grep 'SHA-256' || true)"
  if [ -n "${SHA256_LINE}" ]; then
    status_line "signing fingerprint" "${SHA256_LINE#*: }"
  else
    status_line "signing fingerprint" "unavailable"
  fi
fi

check_env_var AIBOT_ANDROID_FIREBASE_API_KEY || PREFLIGHT_FAILED=1
check_env_var AIBOT_ANDROID_FIREBASE_APP_ID || PREFLIGHT_FAILED=1
check_env_var AIBOT_ANDROID_FIREBASE_PROJECT_ID || PREFLIGHT_FAILED=1
check_env_var AIBOT_ANDROID_FIREBASE_SENDER_ID || PREFLIGHT_FAILED=1
check_env_var AIBOT_PUSH_JPUSH_APP_KEY || PREFLIGHT_FAILED=1
# 华为 Push Kit：App ID 缺失不会让构建失败，而是让华为设备静默降级到极光
# （杀进程收不到推送），必须在出包前硬拦。
check_env_var AIBOT_ANDROID_HUAWEI_APP_ID || PREFLIGHT_FAILED=1

read_push_param() {
  local name="$1" file
  for file in "${ENV_FILE}" "${ANDROID_ENV_FILE}"; do
    [ -f "${file}" ] || continue
    local value
    value="$(awk -F= -v target="${name}" '$1 == target { sub(/^[^=]*=/, ""); print; exit }' "${file}")"
    if [ -n "${value}" ]; then
      printf '%s' "${value}"
      return 0
    fi
  done
  return 1
}

# 华为 App ID 形状校验：纯数字。挡住"从 AGC 复制错字段"这类手滑。
HUAWEI_APP_ID_VALUE="$(read_push_param AIBOT_ANDROID_HUAWEI_APP_ID || true)"
if [ -n "${HUAWEI_APP_ID_VALUE}" ]; then
  case "${HUAWEI_APP_ID_VALUE}" in
    ''|*[!0-9]*)
      status_line "huawei app id shape" "not numeric: ${HUAWEI_APP_ID_VALUE}"
      PREFLIGHT_FAILED=1
      ;;
    *)
      status_line "huawei app id shape" "numeric"
      ;;
  esac
fi

# 两侧一致性：APK 里的 AIBOT_ANDROID_HUAWEI_APP_ID 与服务端 secret 的
# AIBOT_PUSH_HUAWEI_APP_ID 必须同值，否则设备把 token 注册到 A、服务端拿 B 下发，
# 全链路静默失败。本机有真实（非 example）secret 时强校验；没有则跳过。
# 可用 BACKEND_SECRET_FILE 指向私有运维仓或其他本地忽略的 secret 副本。
if [ -z "${BACKEND_SECRET_FILE:-}" ]; then
  BACKEND_SECRET_FILE="$(release_resolve_repo_local_file "${FRONTEND_ROOT}/.." "k8s/apps/secret.yaml")"
fi
if [ -n "${HUAWEI_APP_ID_VALUE}" ] && [ -f "${BACKEND_SECRET_FILE}" ] && [[ "${BACKEND_SECRET_FILE}" != *example.yaml ]]; then
  BACKEND_HUAWEI_APP_ID="$(sed -n 's/^[[:space:]]*AIBOT_PUSH_HUAWEI_APP_ID:[[:space:]]*//p' "${BACKEND_SECRET_FILE}" | head -1 | sed 's/"//g; s/[[:space:]]*$//')"
  if [ -z "${BACKEND_HUAWEI_APP_ID}" ]; then
    status_line "huawei app id cross-check" "backend secret missing AIBOT_PUSH_HUAWEI_APP_ID"
    PREFLIGHT_FAILED=1
  elif [ "${BACKEND_HUAWEI_APP_ID}" != "${HUAWEI_APP_ID_VALUE}" ]; then
    status_line "huawei app id cross-check" "MISMATCH apk=${HUAWEI_APP_ID_VALUE} backend=${BACKEND_HUAWEI_APP_ID}"
    PREFLIGHT_FAILED=1
  else
    status_line "huawei app id cross-check" "consistent (${HUAWEI_APP_ID_VALUE})"
  fi
elif [ -n "${HUAWEI_APP_ID_VALUE}" ]; then
  if [ -n "${BACKEND_SECRET_FILE:-}" ] && [[ "${BACKEND_SECRET_FILE}" == *example.yaml ]]; then
    status_line "huawei app id cross-check" "skipped (refusing example placeholder secret)"
  else
    status_line "huawei app id cross-check" "skipped (backend secret file not on this machine)"
  fi
fi

if [ "${PREFLIGHT_FAILED}" -ne 0 ]; then
  echo "[preflight] failed: missing required Android release prerequisites" >&2
  exit 1
fi
