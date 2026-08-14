#!/usr/bin/env bash
set -euo pipefail

# 本脚本只跑本地 CI 校验，不发布任何产物。
# 等效于 .github/workflows/ci-backend.yml：构建 + vet + 单元测试。

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT_DIR}/backend"

export PATH="$PATH:/usr/local/go/bin"
export AIBOT_TEST_NATS_URL="nats://127.0.0.1:1"

mkdir -p internal/webapp/dist
if [ ! -f internal/webapp/dist/index.html ]; then
  printf '<!doctype html><title>stub</title>' > internal/webapp/dist/index.html
fi

go build ./...
go vet ./...
go test ./...
