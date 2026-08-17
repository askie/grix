#!/usr/bin/env bash
set -euo pipefail

# publish_app_release.sh — 自动创建并发布 app_release 记录
# 用法: ./scripts/publish_app_release.sh --platform <platform> --download-url <url> [选项]
#
# 必需环境变量:
#   ADMIN_API_BASE_URL  — 后端 Admin API 地址（如 https://grix.dhf.pub/admin/api）
#   ADMIN_USERNAME      — 管理员用户名
#   ADMIN_PASSWORD      — 管理员密码
#
# 参数:
#   --platform        ios | android | macos | windows | linux
#   --version         版本号（默认从 pubspec.yaml 读取）
#   --build-number    构建号（默认从 pubspec.yaml 读取）
#   --channel         stable | beta（默认 stable）
#   --download-url    下载链接
#   --app-store-url   应用商店链接（可选）
#   --update-method   download | app_store | google_play（默认 download）
#   --changelog       更新日志（可选）
#   --file-path       本地文件路径（自动计算 file_size 和 sha256）
#   --auto-publish    创建后自动发布（默认开启）
#   --no-publish      仅创建 draft，不自动发布

log() { echo "[publish-release] $*" >&2; }
fail() { echo "[publish-release] ERROR: $*" >&2; exit 1; }

# curl 包装：执行请求并校验返回为 HTTP 2xx + JSON，否则打印诊断（含响应体前 500 字）后退出。
# 这样当后端被 CDN/WAF（如 Cloudflare 对数据中心 IP 弹人机校验 HTML）拦截、返回非 JSON 时，
# 报错里能直接看到后端到底返回了什么，而不是只剩一行难懂的 jq parse error。
# 用法: body="$(http_request_json <描述> <curl 参数...>)"
http_request_json() {
  local desc="$1"; shift
  local raw meta body code ct
  # 在响应体末尾附加一行 "<http_code>\t<content_type>" 以便提取
  raw="$(curl -sS -m 30 -w $'\n%{http_code}\t%{content_type}' "$@")" \
    || fail "${desc}: 请求失败（网络/连接错误，未拿到响应）"
  meta="${raw##*$'\n'}"   # 最后一行：状态码与类型
  body="${raw%$'\n'*}"    # 其余：响应体
  code="${meta%%$'\t'*}"
  ct="${meta#*$'\t'}"
  if [[ ! "${code}" =~ ^2[0-9][0-9]$ ]]; then
    fail "${desc}: HTTP ${code} (content-type: ${ct})；响应体: $(printf '%.500s' "${body}")"
  fi
  case "${ct}" in
    application/json*) : ;;
    *) fail "${desc}: 期望 JSON，实际 content-type=${ct} (HTTP ${code})——很可能被 CDN/WAF 拦截。响应体前 500 字符: $(printf '%.500s' "${body}")" ;;
  esac
  printf '%s' "${body}"
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
PUBSPEC="${ROOT_DIR}/frontend/pubspec.yaml"

# 默认值
PLATFORM=""
VERSION=""
BUILD_NUMBER=""
CHANNEL="stable"
DOWNLOAD_URL=""
APP_STORE_URL=""
UPDATE_METHOD="download"
CHANGELOG=""
FILE_PATH=""
AUTO_PUBLISH=1
FILE_SIZE=0
SHA256=""
EDDSA_SIGNATURE=""

# 解析参数
while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform) PLATFORM="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --build-number) BUILD_NUMBER="$2"; shift 2 ;;
    --channel) CHANNEL="$2"; shift 2 ;;
    --download-url) DOWNLOAD_URL="$2"; shift 2 ;;
    --app-store-url) APP_STORE_URL="$2"; shift 2 ;;
    --update-method) UPDATE_METHOD="$2"; shift 2 ;;
    --changelog) CHANGELOG="$2"; shift 2 ;;
    --file-path) FILE_PATH="$2"; shift 2 ;;
    --eddsa-signature) EDDSA_SIGNATURE="$2"; shift 2 ;;
    --auto-publish) AUTO_PUBLISH=1; shift ;;
    --no-publish) AUTO_PUBLISH=0; shift ;;
    *) fail "未知参数: $1" ;;
  esac
done

# 校验
[[ -n "${PLATFORM}" ]] || fail "--platform 必须指定"
[[ -n "${ADMIN_API_BASE_URL:-}" ]] || fail "环境变量 ADMIN_API_BASE_URL 未设置"
[[ -n "${ADMIN_USERNAME:-}" ]] || fail "环境变量 ADMIN_USERNAME 未设置"
[[ -n "${ADMIN_PASSWORD:-}" ]] || fail "环境变量 ADMIN_PASSWORD 未设置"

# 从 pubspec.yaml 读取版本号
if [[ -z "${VERSION}" || -z "${BUILD_NUMBER}" ]]; then
  [[ -f "${PUBSPEC}" ]] || fail "pubspec.yaml 不存在: ${PUBSPEC}"
  FULL_VERSION="$(grep -E '^version:' "${PUBSPEC}" | head -1 | awk '{print $2}')"
  if [[ -z "${VERSION}" ]]; then
    VERSION="${FULL_VERSION%%+*}"
  fi
  if [[ -z "${BUILD_NUMBER}" ]]; then
    BUILD_NUMBER="${FULL_VERSION##*+}"
  fi
fi

[[ -n "${VERSION}" ]] || fail "无法确定版本号"
[[ -n "${BUILD_NUMBER}" ]] || fail "无法确定构建号"
# build_number 经 --argjson 传入 jq，非纯数字会被当 JSON 解析破坏请求体，提前拦截
[[ "${BUILD_NUMBER}" =~ ^[0-9]+$ ]] || fail "构建号必须是纯数字: ${BUILD_NUMBER}"

# 从文件计算 size 和 sha256
if [[ -n "${FILE_PATH}" && -f "${FILE_PATH}" ]]; then
  FILE_SIZE="$(wc -c < "${FILE_PATH}" | tr -d ' ')"
  SHA256="$(shasum -a 256 "${FILE_PATH}" | awk '{print $1}')"
  log "文件: ${FILE_PATH} (${FILE_SIZE} bytes, sha256=${SHA256})"
fi

# 登录获取 token
log "登录 Admin API..."
# 密码不进 curl 命令行参数（同机进程经 ps 可见）：jq 生成 body，经 stdin 传入
LOGIN_RESP="$(jq -n --arg username "${ADMIN_USERNAME}" --arg password "${ADMIN_PASSWORD}" \
  '{username: $username, password: $password}' | \
  http_request_json "登录 Admin API (${ADMIN_API_BASE_URL}/login)" \
  -X POST "${ADMIN_API_BASE_URL}/login" \
  -H 'Content-Type: application/json' \
  --data-binary @-)"

TOKEN="$(echo "${LOGIN_RESP}" | jq -r '.data.token // .token // empty')"
[[ -n "${TOKEN}" ]] || fail "登录失败（响应为 JSON 但缺少 token，可能凭据无效）: ${LOGIN_RESP}"
log "登录成功"

# 创建 release
log "创建 app_release: platform=${PLATFORM} version=${VERSION}+${BUILD_NUMBER} method=${UPDATE_METHOD}"
CREATE_BODY="$(jq -n \
  --arg version "${VERSION}" \
  --argjson build_number "${BUILD_NUMBER}" \
  --arg platform "${PLATFORM}" \
  --arg channel "${CHANNEL}" \
  --arg changelog "${CHANGELOG}" \
  --arg update_method "${UPDATE_METHOD}" \
  --arg download_url "${DOWNLOAD_URL}" \
  --arg app_store_url "${APP_STORE_URL}" \
  --argjson file_size "${FILE_SIZE}" \
  --arg sha256 "${SHA256}" \
  --arg eddsa_signature "${EDDSA_SIGNATURE}" \
  '{
    version: $version,
    build_number: $build_number,
    platform: $platform,
    channel: $channel,
    changelog: $changelog,
    update_method: $update_method,
    download_url: $download_url,
    app_store_url: $app_store_url,
    file_size: $file_size,
    sha256: $sha256,
    eddsa_signature: $eddsa_signature
  }')"

# 同 platform/channel/version/build 已存在时后端返回 409（如 iOS 已由
# 本机 release.sh 先行创建）：多平台循环发布中视为已完成而非失败，
# 否则一个平台重复会挡住其余平台。
CREATE_RAW="$(curl -sS -m 30 -w $'\n%{http_code}\t%{content_type}' \
  -X POST "${ADMIN_API_BASE_URL}/app/releases" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "${CREATE_BODY}")" || fail "创建 app_release: 请求失败（网络/连接错误）"
CREATE_META="${CREATE_RAW##*$'\n'}"
CREATE_RESP="${CREATE_RAW%$'\n'*}"
CREATE_CODE="${CREATE_META%%$'\t'*}"

if [[ "${CREATE_CODE}" == "409" ]]; then
  log "已存在同版本记录，跳过: ${PLATFORM} ${VERSION}+${BUILD_NUMBER}"
  exit 0
fi
[[ "${CREATE_CODE}" =~ ^2[0-9][0-9]$ ]] \
  || fail "创建 app_release: HTTP ${CREATE_CODE}；响应体: $(printf '%.500s' "${CREATE_RESP}")"

RELEASE_ID="$(echo "${CREATE_RESP}" | jq -r '.data.release.id // empty')"
[[ -n "${RELEASE_ID}" ]] || fail "创建失败: ${CREATE_RESP}"
log "创建成功: id=${RELEASE_ID}"

# 自动发布
if [[ "${AUTO_PUBLISH}" == "1" ]]; then
  log "发布 release ${RELEASE_ID}..."
  PUBLISH_RESP="$(http_request_json "发布 release ${RELEASE_ID}" \
    -X POST "${ADMIN_API_BASE_URL}/app/releases/${RELEASE_ID}/publish" \
    -H "Authorization: Bearer ${TOKEN}")"

  if [[ "$(echo "${PUBLISH_RESP}" | jq -r '.data.ok // empty')" == "true" ]]; then
    log "发布成功: ${PLATFORM} ${VERSION}+${BUILD_NUMBER}"
  else
    fail "发布失败: ${PUBLISH_RESP}"
  fi
fi

# 输出 release_id 供后续使用
echo "${RELEASE_ID}"
