#!/usr/bin/env bash
# embed_frontends.sh —— "api 镜像必须嵌入哪些前端"的唯一定义（收口点）。
#
# api 二进制通过 go:embed 携带两个前端产物，缺一不可：
#   1) 用户端 Flutter Web  → backend/internal/webapp/dist   （sync_web_dist.sh）
#   2) 塘主管理后台        → backend/internal/adminweb/dist （sync_admin_dist.sh）
#
# 所有构建 api 镜像的链路（CN k8s 部署脚本、本机 Tag 发布流程）一律调用本脚本，
# 不得单独调用 sync_web_dist.sh / sync_admin_dist.sh —— 历史教训：全球区构建
# 只同步了用户端 Web，漏了塘主后台，线上 /admin 是旧版本。
#
# 环境变量（原样透传给对应子脚本，均有安全默认值）：
#   用户端: WEB_BUILD_MODE BASE_HREF API_BASE_URL WS_URL CN_API_URL CN_WS_URL
#           GLOBAL_API_URL GLOBAL_WS_URL WEB_PUSH_VAPID_PUBLIC_KEY WEB_USE_WASM
#   塘主:   ADMIN_WEB_BUILD_MODE ADMIN_WEB_API_BASE_URL（默认空 = 相对路径，区域无关）
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"

echo "[embed_frontends] sync user web dist"
BUILD_MODE="${WEB_BUILD_MODE:-release}" \
	bash "${SCRIPT_DIR}/sync_web_dist.sh"

echo "[embed_frontends] sync admin (塘主) web dist"
BUILD_MODE="${ADMIN_WEB_BUILD_MODE:-release}" \
	ADMIN_API_BASE_URL="${ADMIN_WEB_API_BASE_URL:-}" \
	bash "${SCRIPT_DIR}/sync_admin_dist.sh"

echo "[embed_frontends] both frontends embedded"
