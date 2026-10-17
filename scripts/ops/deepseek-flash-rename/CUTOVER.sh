#!/usr/bin/env bash
# DeepSeek flash rename cutover helper (CN TKE + Global AWS).
#
# Correct order (do NOT reverse):
#   1) 01_seed_pricing.sql          # safe anytime; required BEFORE/WITH code deploy
#   2) deploy gateway+api with CanonicalModel → deepseek-flash
#   3) 02_migrate_user_data.sql     # before closing old pricing
#   4) 03_close_old_pricing.sql
#
# Usage examples:
#   ./CUTOVER.sh cn seed
#   ./CUTOVER.sh global seed
#   ./CUTOVER.sh cn migrate
#   ./CUTOVER.sh cn close
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
REGION="${1:-}"
STEP="${2:-}"

usage() {
  echo "usage: $0 {cn|global} {seed|migrate|close|status}" >&2
  exit 2
}

[[ -n "$REGION" && -n "$STEP" ]] || usage

run_sql() {
  local file="$1"
  case "$REGION" in
    cn)
      local pw
      pw="$(kubectl -n clawpool get secret aibot-infra-env -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d)"
      kubectl -n clawpool exec -i aibot-postgres-0 -- \
        env PGPASSWORD="$pw" psql -U aibot -d aibot -v ON_ERROR_STOP=1 <"$file"
      ;;
    global)
      local pw
      pw="$(kubectl --kubeconfig "${KUBECONFIG_GLOBAL:-$HOME/.kube/aws-k3s.yaml}" -n grix \
        get secret aibot-infra-env -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d)"
      kubectl --kubeconfig "${KUBECONFIG_GLOBAL:-$HOME/.kube/aws-k3s.yaml}" -n grix exec -i aibot-postgres-0 -- \
        env PGPASSWORD="$pw" psql -U aibot -d aibot -v ON_ERROR_STOP=1 <"$file"
      ;;
    *) usage ;;
  esac
}

status_sql() {
  cat <<'SQL'
SELECT model,
       count(*) FILTER (WHERE effective_to IS NULL) AS active
FROM gateway_pricing_rules
WHERE provider='deepseek' AND model IN ('deepseek-flash','deepseek-v4-flash','deepseek-v4-pro')
GROUP BY model ORDER BY model;

SELECT count(*) FILTER (WHERE default_model='deepseek-v4-flash') AS settings_old,
       count(*) FILTER (WHERE default_model='deepseek-flash') AS settings_new
FROM gateway_relay_settings;

SELECT count(*) FILTER (WHERE relay_model LIKE '%deepseek-v4-flash%' OR relay_model LIKE '%DeepSeek-V4-Flash%') AS state_old,
       count(*) FILTER (WHERE relay_model='deepseek-flash') AS state_new
FROM gateway_agent_relay_state;

SELECT count(*) FILTER (WHERE status='active' AND (relay_model LIKE '%deepseek-v4-flash%' OR relay_model LIKE '%DeepSeek-V4-Flash%')) AS vk_old,
       count(*) FILTER (WHERE status='active' AND relay_model='deepseek-flash') AS vk_new
FROM gateway_virtual_keys;
SQL
}

case "$STEP" in
  seed) run_sql "$ROOT/01_seed_pricing.sql" ;;
  migrate) run_sql "$ROOT/02_migrate_user_data.sql" ;;
  close) run_sql "$ROOT/03_close_old_pricing.sql" ;;
  status)
    tmp="$(mktemp)"
    status_sql >"$tmp"
    run_sql "$tmp"
    rm -f "$tmp"
    ;;
  *) usage ;;
esac
