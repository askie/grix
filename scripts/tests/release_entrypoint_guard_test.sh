#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fail_test() { echo "[release-entrypoint-guard-test] FAIL: $*" >&2; exit 1; }

fake_ops="${tmp_dir}/grix-ops"
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_ops}/scripts" "${fake_bin}"
printf '#!/usr/bin/env bash\n' > "${fake_ops}/scripts/common_release_env.sh"
printf '#!/usr/bin/env bash\nexit 0\n' > "${fake_bin}/flutter"
printf '#!/usr/bin/env bash\nexit 0\n' > "${fake_bin}/xcrun"
chmod +x "${fake_bin}/flutter" "${fake_bin}/xcrun"

# The current typed grix-ops frontend entrypoint contract is accepted.
GRIX_OPS_DIR="${fake_ops}" GRIX_RELEASE_UNIFIED_CALL=1 \
  GRIX_RELEASE_ENTRYPOINT=frontend bash -c '
    source "$1"
    require_unified_release_call guard-test release-frontend
  ' _ "${ROOT_DIR}/frontend/scripts/common_push_env.sh" || \
  fail_test "typed frontend entrypoint was rejected"

# An exported flag alone must not bypass the frontend parent-process guard.
set +e
GRIX_OPS_DIR="${fake_ops}" GRIX_RELEASE_UNIFIED_CALL=1 bash -c '
  source "$1"
  require_unified_release_call guard-test release-frontend
' _ "${ROOT_DIR}/frontend/scripts/common_push_env.sh" >/dev/null 2>&1
direct_rc=$?
set -e
[[ ${direct_rc} -ne 0 ]] || fail_test "direct frontend invocation was accepted"

# Admin build and upload accept the new flag while keeping the old flag as a
# compatibility fallback. Fake tools keep this contract test side-effect free.
GRIX_OPS_DIR="${fake_ops}" GRIX_RELEASE_UNIFIED_CALL=1 GRIX_RELEASE_ENTRYPOINT=frontend \
  PATH="${fake_bin}:${PATH}" \
  bash "${ROOT_DIR}/admin/scripts/ios_push_build.sh" run >/dev/null

ipa_path="${tmp_dir}/Admin.ipa"
printf 'fixture\n' > "${ipa_path}"
GRIX_RELEASE_UNIFIED_CALL=1 GRIX_RELEASE_ENTRYPOINT=frontend PATH="${fake_bin}:${PATH}" \
  APPLE_ID=test@example.invalid APPLE_APP_PASSWORD=test-password \
  bash "${ROOT_DIR}/admin/scripts/ios_upload_testflight.sh" "${ipa_path}" >/dev/null

echo "[release-entrypoint-guard-test] PASS"
