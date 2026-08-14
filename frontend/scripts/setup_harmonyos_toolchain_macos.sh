#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
  echo "[harmonyos-setup] this script only supports macOS" >&2
  exit 1
fi

if ! command -v brew >/dev/null 2>&1; then
  echo "[harmonyos-setup] Homebrew is required: https://brew.sh" >&2
  exit 1
fi

if ! command -v git >/dev/null 2>&1; then
  echo "[harmonyos-setup] git is required and must be on PATH" >&2
  exit 1
fi

FRONTEND_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

OHOS_HOME="${OHOS_HOME:-${HOME}/development/ohos}"
OHOS_FLUTTER_HOME="${OHOS_FLUTTER_HOME:-${OHOS_HOME}/flutter_flutter}"
OHOS_FLUTTER_REPO="${OHOS_FLUTTER_REPO:-https://gitee.com/openharmony-sig/flutter_flutter.git}"
OHOS_FLUTTER_REF="${OHOS_FLUTTER_REF:-3.7.12-ohos-1.0.3}"

DEVECO_APP="${DEVECO_APP:-/Applications/DevEco-Studio.app}"
TOOL_HOME="${TOOL_HOME:-${DEVECO_APP}/Contents}"
OHOS_COMMANDLINE_HOME="${OHOS_COMMANDLINE_HOME:-${OHOS_HOME}/command-line-tools}"
DEVECO_SDK_HOME_DEFAULT="${DEVECO_SDK_HOME:-${TOOL_HOME}/sdk}"
OHOS_SDK_HOME_DEFAULT="${OHOS_SDK_HOME:-${OHOS_HOME}/sdk/openharmony}"
OHOS_HVIGOR_VERSION="${OHOS_HVIGOR_VERSION:-6.22.3}"

resolve_java_home() {
  local homebrew_candidate="/opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home"
  local intel_candidate="/usr/local/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home"

  if [ -d "${homebrew_candidate}" ]; then
    echo "${homebrew_candidate}"
    return
  fi

  if [ -d "${intel_candidate}" ]; then
    echo "${intel_candidate}"
    return
  fi

  if [ -n "${JAVA_HOME:-}" ]; then
    echo "${JAVA_HOME}"
    return
  fi

  echo "/opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home"
}

JAVA_HOME_CANDIDATE="$(resolve_java_home)"

log() {
  echo "[harmonyos-setup] $*"
}

have_java_17() {
  if ! command -v java >/dev/null 2>&1; then
    return 1
  fi

  local version_line
  version_line="$(java -version 2>&1 | head -n 1 || true)"
  [[ "${version_line}" == *"17."* || "${version_line}" == *"\"17"* ]]
}

install_java_17() {
  if have_java_17; then
    log "java 17 already available"
    return
  fi

  log "installing JDK 17"
  brew install openjdk@17
}

clone_or_update_flutter_ohos() {
  mkdir -p "${OHOS_HOME}"

  if [ ! -d "${OHOS_FLUTTER_HOME}/.git" ]; then
    log "cloning OpenHarmony Flutter SDK to ${OHOS_FLUTTER_HOME}"
    git clone --branch "${OHOS_FLUTTER_REF}" --single-branch "${OHOS_FLUTTER_REPO}" "${OHOS_FLUTTER_HOME}"
    return
  fi

  log "updating OpenHarmony Flutter SDK in ${OHOS_FLUTTER_HOME}"
  git -C "${OHOS_FLUTTER_HOME}" fetch --tags origin
  git -C "${OHOS_FLUTTER_HOME}" checkout "${OHOS_FLUTTER_REF}"

  if git -C "${OHOS_FLUTTER_HOME}" rev-parse --verify --quiet "origin/${OHOS_FLUTTER_REF}" >/dev/null; then
    git -C "${OHOS_FLUTTER_HOME}" pull --ff-only origin "${OHOS_FLUTTER_REF}"
  else
    log "checked out tag/ref ${OHOS_FLUTTER_REF}"
  fi
}

resolve_node_home() {
  local node_path
  node_path="$(command -v node 2>/dev/null || true)"
  if [ -z "${node_path}" ]; then
    return 1
  fi

  dirname "$(dirname "${node_path}")"
}

install_hvigor_cli() {
  if ! command -v node >/dev/null 2>&1; then
    log "node was not found on PATH; skipping global hvigor install"
    return
  fi

  if ! command -v npm >/dev/null 2>&1; then
    log "npm was not found on PATH; skipping global hvigor install"
    return
  fi

  log "installing hvigor cli packages"
  npm install -g \
    "@ohos/hvigor@${OHOS_HVIGOR_VERSION}" \
    "@ohos/hvigor-ohos-plugin@${OHOS_HVIGOR_VERSION}" \
    --registry=https://repo.harmonyos.com/npm/

  local npm_root node_bin hvigor_js
  npm_root="$(npm root -g)"
  node_bin="$(dirname "$(command -v node)")"
  hvigor_js="${npm_root}/@ohos/hvigor/bin/hvigor.js"

  if [ ! -f "${hvigor_js}" ]; then
    log "hvigor package was installed but ${hvigor_js} was not found"
    return
  fi

  cat > "${node_bin}/hvigorw" <<EOF
#!/bin/sh
exec node "${hvigor_js}" "\$@"
EOF

  cat > "${node_bin}/hvigor" <<EOF
#!/bin/sh
exec node "${hvigor_js}" "\$@"
EOF

  chmod 755 "${node_bin}/hvigorw" "${node_bin}/hvigor"
  log "installed hvigor wrappers into ${node_bin}"
}

print_env_block() {
  local node_home_line=""
  local node_path_line=""
  local detected_node_home

  detected_node_home="$(resolve_node_home || true)"
  if [ -n "${detected_node_home}" ]; then
    node_home_line="export NODE_HOME=\"${detected_node_home}\""
    node_path_line="export PATH=\"${detected_node_home}/bin:\$PATH\""
  fi

  cat <<EOF
export JAVA_HOME="${JAVA_HOME_CANDIDATE}"
export TOOL_HOME="${TOOL_HOME}"
export DEVECO_SDK_HOME="${DEVECO_SDK_HOME_DEFAULT}"
export OHOS_COMMANDLINE_HOME="${OHOS_COMMANDLINE_HOME}"
export OHOS_SDK_HOME="${OHOS_SDK_HOME_DEFAULT}"
${node_home_line}
export PATH="\$JAVA_HOME/bin:\$PATH"
export PATH="\$OHOS_COMMANDLINE_HOME/bin:\$PATH"
export PATH="\$OHOS_COMMANDLINE_HOME/ohpm/bin:\$PATH"
export PATH="\$OHOS_SDK_HOME/9/toolchains:\$PATH"
${node_path_line}
export PATH="${OHOS_FLUTTER_HOME}/bin:\$PATH"
export PUB_HOSTED_URL="https://pub.flutter-io.cn"
export FLUTTER_STORAGE_BASE_URL="https://storage.flutter-io.cn"
export FLUTTER_GIT_URL="${OHOS_FLUTTER_REPO}"
EOF
}

print_manual_next_steps() {
  if [ ! -d "${DEVECO_APP}" ] && [ ! -d "${OHOS_COMMANDLINE_HOME}" ]; then
    cat <<EOF
[harmonyos-setup] Neither DevEco Studio nor OpenHarmony command-line tools were found.

Expected one of:
  ${DEVECO_APP}
  ${OHOS_COMMANDLINE_HOME}

Install HarmonyOS command-line tools or DevEco Studio from Huawei official channels, then rerun:
  ${FRONTEND_ROOT}/scripts/harmonyos_preflight.sh

Official docs:
  https://developer.huawei.com/consumer/cn/doc/harmonyos-guides/ide-software-install
EOF
    return
  fi

  cat <<EOF
[harmonyos-setup] DevEco Studio detected:
  ${DEVECO_APP}

Copy these exports into your shell before building HarmonyOS:
$(print_env_block)

Validation:
  cd ${FRONTEND_ROOT}
  ./scripts/harmonyos_preflight.sh
  flutter doctor -v

When preflight passes:
  cd ${FRONTEND_ROOT}
  flutter create --platforms ohos .
  flutter build hap --release
EOF
}

main() {
  log "frontend root: ${FRONTEND_ROOT}"
  log "target OpenHarmony Flutter ref: ${OHOS_FLUTTER_REF}"

  install_java_17
  clone_or_update_flutter_ohos
  install_hvigor_cli
  print_manual_next_steps
}

main "$@"
