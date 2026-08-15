#!/usr/bin/env bash

release_prepend_path_dir() {
  local dir="${1:-}"
  [[ -n "${dir}" && -d "${dir}" ]] || return 1

  case ":${PATH:-}:" in
    *":${dir}:"*)
      return 0
      ;;
  esac

  export PATH="${dir}:${PATH:-}"
}

release_ensure_command_on_path() {
  local cmd="$1"
  shift

  if command -v "${cmd}" >/dev/null 2>&1; then
    return 0
  fi

  local candidate
  for candidate in "$@"; do
    [[ -x "${candidate}" ]] || continue
    release_prepend_path_dir "$(dirname "${candidate}")" || continue
    if command -v "${cmd}" >/dev/null 2>&1; then
      return 0
    fi
  done

  return 1
}

release_ensure_flutter_on_path() {
  local candidates=(
    "${HOME}/development/flutter/bin/flutter"
    "${HOME}/fvm/default/bin/flutter"
    "/opt/homebrew/bin/flutter"
    "/usr/local/bin/flutter"
  )
  release_ensure_command_on_path flutter "${candidates[@]}"
}

release_ensure_docker_on_path() {
  local candidates=(
    "/Applications/Docker.app/Contents/Resources/bin/docker"
    "/opt/homebrew/bin/docker"
    "/usr/local/bin/docker"
  )
  release_ensure_command_on_path docker "${candidates[@]}"
}

release_ensure_kubectl_on_path() {
  local candidates=(
    "/Applications/Docker.app/Contents/Resources/bin/kubectl"
    "/opt/homebrew/bin/kubectl"
    "/usr/local/bin/kubectl"
  )
  release_ensure_command_on_path kubectl "${candidates[@]}"
}

release_ensure_pod_on_path() {
  local candidates=(
    "/opt/homebrew/bin/pod"
    "/usr/local/bin/pod"
  )
  local candidate

  shopt -s nullglob
  for candidate in "${HOME}"/.gem/ruby/*/bin/pod; do
    candidates+=("${candidate}")
  done
  shopt -u nullglob

  release_ensure_command_on_path pod "${candidates[@]}"
}

release_ensure_cocoapods_rubyopt() {
  if ! command -v pod >/dev/null 2>&1; then
    return 0
  fi

  case " ${RUBYOPT:-} " in
    *" -rlogger "*)
      return 0
      ;;
  esac

  export RUBYOPT="${RUBYOPT:+${RUBYOPT} }-rlogger"
}

release_git_common_root() {
  local repo_root="${1:-}"
  local git_common_dir

  [[ -n "${repo_root}" ]] || return 1
  command -v git >/dev/null 2>&1 || return 1

  git_common_dir="$(git -C "${repo_root}" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  [[ -n "${git_common_dir}" ]] || return 1

  cd "$(dirname "${git_common_dir}")" && pwd
}

release_export_git_common_root() {
  local repo_root="${1:-}"
  local common_root

  common_root="$(release_git_common_root "${repo_root}")" || return 1
  export AIBOT_GIT_COMMON_ROOT="${common_root}"
  printf '%s\n' "${common_root}"
}

release_resolve_repo_local_file() {
  local repo_root="${1:-}"
  local relative_path="${2:-}"
  local local_path common_root common_path

  [[ -n "${repo_root}" && -n "${relative_path}" ]] || return 1

  local_path="${repo_root}/${relative_path}"
  if [[ -e "${local_path}" ]]; then
    printf '%s\n' "${local_path}"
    return 0
  fi

  common_root="$(release_git_common_root "${repo_root}")" || true
  if [[ -n "${common_root}" ]]; then
    common_path="${common_root}/${relative_path}"
    if [[ -e "${common_path}" ]]; then
      printf '%s\n' "${common_path}"
      return 0
    fi
  fi

  printf '%s\n' "${local_path}"
}

release_resolve_push_env_file() {
  local frontend_root="${1:-}"
  local repo_root

  [[ -n "${frontend_root}" ]] || return 1
  repo_root="$(cd "${frontend_root}/.." && pwd)"
  release_resolve_repo_local_file "${repo_root}" "frontend/.env.push.local"
}

release_ensure_colima_ready() {
  # 如果 Docker Desktop 正在运行，不干预
  if command -v docker >/dev/null 2>&1 && docker info --format '{{.OperatingSystem}}' 2>/dev/null | grep -qi 'docker desktop'; then
    return 0
  fi

  local colima_bin
  colima_bin="$(command -v colima 2>/dev/null || true)"
  if [[ -z "${colima_bin}" ]]; then
    # colima 未安装，走已有 Docker
    return 0
  fi

  # 用退出码判断运行态最可靠：colima status 运行中 exit 0，未运行 exit 1。
  # 不能用 grep 'running' 匹配文本——未运行时的 "colima is not running" 也含
  # "running" 子串，会误判为已运行导致不启动。
  if "${colima_bin}" status >/dev/null 2>&1; then
    log "colima VM 运行中"
    return 0
  fi

  log "colima VM 未运行，自动启动..."
  "${colima_bin}" start 2>&1 || {
    echo "[release] WARN: colima start 失败，使用已有 Docker 继续" >&2
    return 0
  }
  # 标记 colima 是本次发布流程启动的：发布结束后 release_shutdown_colima_if_script_started
  # 只停这种"用完即走"的实例；用户手动起的 colima（上面可能挂着业务容器）绝不停。
  export AIBOT_RELEASE_COLIMA_STARTED=1

  local wait_seconds=30 waited=0
  while ! docker info >/dev/null 2>&1; do
    sleep 1
    waited=$((waited + 1))
    if (( waited >= wait_seconds )); then
      echo "[release] WARN: colima 已启动但 Docker 未就绪（${wait_seconds}s），继续" >&2
      break
    fi
  done
  if (( waited < wait_seconds )); then
    log "colima VM 就绪（${waited}s）"
  fi
}

# 发布流程结束后关闭 colima：镜像编译完了就关掉，下次编译时 release_ensure_colima_ready
# 会自动拉起。避免 colima VM 长期驻留导致的各种问题（残留 usernet 进程、网络转发错乱等）。
# 安全闸门（三条都满足才停）：
#   1) colima 必须是本次发布流程启动的（AIBOT_RELEASE_COLIMA_STARTED=1）——
#      用户手动启动的 colima 不动，它上面可能跑着本地业务容器（如 postgres）。
#   2) colima 上不能有运行中的容器——构建缓存镜像不算（镜像不占用运行态），
#      但任何 running 容器都视为业务负载，跳过停机并警告。
#   3) AIBOT_RELEASE_KEEP_COLIMA != 1（显式要求保留时跳过）。
release_shutdown_colima_if_script_started() {
  if [[ "${AIBOT_RELEASE_COLIMA_SHUTDOWN_ATTEMPTED:-0}" == "1" ]]; then
    return 0
  fi

  # 逃生口：明确要求保留 colima
  if [[ "${AIBOT_RELEASE_KEEP_COLIMA:-0}" == "1" ]]; then
    export AIBOT_RELEASE_COLIMA_SHUTDOWN_ATTEMPTED=1
    log "AIBOT_RELEASE_KEEP_COLIMA=1，保留 colima 不关机"
    return 0
  fi

  # 非本流程启动的 colima 不动（Docker Desktop、用户手动 colima 都走这里跳过）
  if [[ "${AIBOT_RELEASE_COLIMA_STARTED:-0}" != "1" ]]; then
    return 0
  fi
  export AIBOT_RELEASE_COLIMA_SHUTDOWN_ATTEMPTED=1

  local colima_bin
  colima_bin="$(command -v colima 2>/dev/null || true)"
  if [[ -z "${colima_bin}" ]]; then
    return 0
  fi

  # 停机前最后确认：colima 上有运行中容器 = 可能有业务负载，不碰
  local running
  running="$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')"
  if [[ -n "${running}" && "${running}" != "0" ]]; then
    echo "[release] WARN: colima 上仍有 ${running} 个运行中容器，跳过关机以免误伤业务负载" >&2
    return 0
  fi

  log "本次构建由脚本启动 colima，发布完成，关闭 colima VM（下次编译时自动重启）..."
  "${colima_bin}" stop 2>&1 || {
    echo "[release] WARN: colima stop 失败（可忽略，下次发布会自动拉起）" >&2
    return 0
  }
  export AIBOT_RELEASE_COLIMA_STARTED=0
  log "colima VM 已关闭"
}

release_set_env_default_if_empty() {
  local name="$1"
  local value="$2"

  if [[ -z "${!name:-}" ]]; then
    printf -v "${name}" '%s' "${value}"
    export "${name}"
    return 0
  fi

  return 1
}

# 区域接口探活守卫：发布前确认 API 地址背后是真后端，而非静态站/死域名。
# 判据：取 origin（去掉结尾 /v1）打 GET /health，真后端固定返回纯文本 "ok"；
# 静态站/废弃域名返回 HTML 或别的内容 → 判定失败、中止发布。
# 这是唯一能拦住"格式合法但无后端"（如全球区误指 grix.im）这类坑的守卫。
# 离线/无网构建可设 SKIP_ENDPOINT_HEALTHCHECK=1 跳过（会打印告警）。
release_require_live_api_backend() {
  local label="$1"
  local api_url="$2"

  if [[ "${SKIP_ENDPOINT_HEALTHCHECK:-0}" == "1" ]]; then
    echo "[release] WARN: 跳过 ${label} 探活 (SKIP_ENDPOINT_HEALTHCHECK=1): ${api_url}" >&2
    return 0
  fi

  if [[ -z "${api_url}" ]]; then
    echo "[release] ✗ ${label} 探活失败：地址为空" >&2
    return 1
  fi

  local origin="${api_url%/}"
  origin="${origin%/v1}"
  origin="${origin%/}"
  local health_url="${origin}/health"

  local body
  # 带重试：单次探活会让一次网络抖动或后端瞬时 5xx 中止整轮发布。--retry-all-errors
  # 是必需的——不加它 curl 只重试超时与 5xx，连接被拒/DNS 失败这类抖动不重试，而那
  # 恰恰是最常见的抖法。守卫要挡的是"地址背后没有后端"，不是"这一秒网不好"。
  body="$(curl -fsS -m 10 --retry 2 --retry-delay 2 --retry-all-errors "${health_url}" 2>/dev/null | tr -d '[:space:]' || true)"
  if [[ "${body}" != "ok" ]]; then
    echo "[release] ✗ ${label} 探活失败：${health_url} 未返回真后端响应（期望 body=ok），实际='${body:0:80}'" >&2
    echo "[release]   该地址可能是静态站/死域名，已中止发布以免出包后该区域不可用。" >&2
    echo "[release]   确认无误需临时跳过：SKIP_ENDPOINT_HEALTHCHECK=1。" >&2
    return 1
  fi

  echo "[release] ✓ ${label} 探活通过：${health_url} -> ok"
  return 0
}
