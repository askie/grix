#!/usr/bin/env bash
set -euo pipefail

# 本脚本只跑本地 CI 校验，不发布任何产物。
# 等效于 .github/workflows/ci-frontend.yml：analyze + 单元测试 + M4 充值隔离检查。

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT_DIR}/frontend"

flutter pub get
flutter analyze --no-fatal-infos
flutter test --timeout 90s

cd "${ROOT_DIR}"
python3 scripts/tests/check_mobile_gateway_isolation.py
# artifact scan 需要先产出 APK；与 .github/workflows/ci-frontend.yml 对齐，默认不做。
# 本地若已构建 APK，可手动执行：
# python3 scripts/tests/check_mobile_gateway_artifact_scan.py frontend/build/app/outputs/flutter-apk
