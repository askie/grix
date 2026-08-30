#!/usr/bin/env bash
set -euo pipefail

# upload_to_oss.sh — 阿里云 OSS 上传脚本（国内下载镜像）
# 纯 shell + openssl 实现 OSS V1 签名，不依赖 ossutil，便于在裸 CI runner 上运行。
# 用法: ./scripts/upload_to_oss.sh <本地文件路径> <OSS目标路径>
# 例:   ./scripts/upload_to_oss.sh release_files/Grix-Android.apk release/2.10.0+635/Grix-Android.apk
#
# 环境变量（必须）:
#   OSS_ACCESS_KEY_ID      — 阿里云 AccessKeyId
#   OSS_ACCESS_KEY_SECRET  — 阿里云 AccessKeySecret
#   OSS_BUCKET             — 桶名（如 grix-release）
#   OSS_ENDPOINT           — 地域 endpoint（如 oss-cn-shanghai.aliyuncs.com）
#
# 环境变量（可选）:
#   OSS_CUSTOM_DOMAIN  — 自定义域名（如 release.dhf.pub），设置后输出该域名的下载 URL
#   OSS_CACHE_CONTROL  — 写入对象的 Cache-Control 头（如 no-store）。release.dhf.pub 前面是
#                        Cloudflare，未配缓存规则时按源站头处理；latest/ 固定链接每次发布都会
#                        被覆盖，必须带 no-store，否则边缘会缓存旧包最多 4 小时
#
# 输出:
#   上传成功后输出下载 URL 到 stdout（最后一行）

# RFC 3986 路径百分号编码（保留 '/'，按字节处理多字节字符）
urlencode_path() {
  local LC_ALL=C
  local path="$1" out="" c i
  for (( i = 0; i < ${#path}; i++ )); do
    c="${path:i:1}"
    case "${c}" in
      [a-zA-Z0-9._~/-]) out+="${c}" ;;
      *)
        # "'c" 取的是有符号字节值，>=0x80 会是负数，先转回 0-255
        printf -v c '%d' "'${c}"
        (( c < 0 )) && c=$(( c + 256 ))
        printf -v c '%%%02X' "${c}"
        out+="${c}" ;;
    esac
  done
  printf '%s' "${out}"
}

log() { echo "[oss-upload] $*" >&2; }
fail() { echo "[oss-upload] ERROR: $*" >&2; exit 1; }

LOCAL_FILE="${1:-}"
OSS_PATH="${2:-}"

[[ -n "${LOCAL_FILE}" ]] || fail "用法: $0 <本地文件路径> <OSS目标路径>"
[[ -n "${OSS_PATH}" ]] || fail "用法: $0 <本地文件路径> <OSS目标路径>"
[[ -f "${LOCAL_FILE}" ]] || fail "文件不存在: ${LOCAL_FILE}"

[[ -n "${OSS_ACCESS_KEY_ID:-}" ]] || fail "环境变量 OSS_ACCESS_KEY_ID 未设置"
[[ -n "${OSS_ACCESS_KEY_SECRET:-}" ]] || fail "环境变量 OSS_ACCESS_KEY_SECRET 未设置"
[[ -n "${OSS_BUCKET:-}" ]] || fail "环境变量 OSS_BUCKET 未设置"
[[ -n "${OSS_ENDPOINT:-}" ]] || fail "环境变量 OSS_ENDPOINT 未设置"

# 去掉前导 /
OSS_PATH="${OSS_PATH#/}"

# URL 用百分号编码后的路径（对象名含空格/非 ASCII 时原样拼 URL 会 403/404）；
# 签名的 CanonicalizedResource 保持原始路径——OSS V1 按解码后的资源串验签。
OSS_PATH_ENC="$(urlencode_path "${OSS_PATH}")"

CONTENT_TYPE="application/octet-stream"
# 签名要求 RFC1123 GMT 时间，星期/月份必须英文，故强制 LC_ALL=C
DATE="$(LC_ALL=C date -u '+%a, %d %b %Y %H:%M:%S GMT')"
RESOURCE="/${OSS_BUCKET}/${OSS_PATH}"

# OSS V1 签名:
#   Signature = base64(HMAC-SHA1(AccessKeySecret, StringToSign))
#   StringToSign = VERB\nContent-MD5\nContent-Type\nDate\nCanonicalizedResource
# 本场景无 x-oss-* 头、无 Content-MD5，对应字段留空。
STRING_TO_SIGN="PUT\n\n${CONTENT_TYPE}\n${DATE}\n${RESOURCE}"
SIGNATURE="$(printf '%b' "${STRING_TO_SIGN}" | openssl dgst -sha1 -hmac "${OSS_ACCESS_KEY_SECRET}" -binary | openssl base64)"

OSS_URL="https://${OSS_BUCKET}.${OSS_ENDPOINT}/${OSS_PATH_ENC}"
RESP_FILE="$(mktemp)"
trap 'rm -f "${RESP_FILE}"' EXIT

log "上传 ${LOCAL_FILE} -> ${OSS_URL}"
HTTP_CODE="$(curl -sS -w '%{http_code}' -o "${RESP_FILE}" \
  -T "${LOCAL_FILE}" \
  -H "Date: ${DATE}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
  ${OSS_CACHE_CONTROL:+-H "Cache-Control: ${OSS_CACHE_CONTROL}"} \
  -H "Authorization: OSS ${OSS_ACCESS_KEY_ID}:${SIGNATURE}" \
  "${OSS_URL}")"

if [[ "${HTTP_CODE}" != "200" ]]; then
  log "上传失败 HTTP ${HTTP_CODE}:"
  cat "${RESP_FILE}" >&2 || true
  fail "OSS 上传失败"
fi
log "上传成功 HTTP 200"
if [[ -n "${OSS_CUSTOM_DOMAIN:-}" ]]; then
  echo "https://${OSS_CUSTOM_DOMAIN}/${OSS_PATH_ENC}"
else
  echo "${OSS_URL}"
fi
