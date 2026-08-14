#!/usr/bin/env bash
set -euo pipefail

android_java_home_candidates() {
  cat <<'EOF'
/opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home
/usr/local/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home
/Library/Java/JavaVirtualMachines/temurin-17.jdk/Contents/Home
/Applications/Android Studio.app/Contents/jbr/Contents/Home
/Applications/Android Studio Preview.app/Contents/jbr/Contents/Home
EOF
}

is_valid_java_home() {
  local candidate="${1:-}"
  [ -n "${candidate}" ] &&
    [ -x "${candidate}/bin/java" ] &&
    [ -x "${candidate}/bin/keytool" ]
}

setup_android_java_home() {
  if is_valid_java_home "${JAVA_HOME:-}"; then
    export PATH="${JAVA_HOME}/bin:${PATH}"
    return 0
  fi

  local candidate
  while IFS= read -r candidate; do
    if is_valid_java_home "${candidate}"; then
      export JAVA_HOME="${candidate}"
      export PATH="${JAVA_HOME}/bin:${PATH}"
      return 0
    fi
  done < <(android_java_home_candidates)

  echo "[android-env] Java runtime not found." >&2
  echo "[android-env] Checked JAVA_HOME and common JDK locations." >&2
  echo "[android-env] Install JDK 17 or Android Studio, or export a valid JAVA_HOME." >&2
  exit 1
}

