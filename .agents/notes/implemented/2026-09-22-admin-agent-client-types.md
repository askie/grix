# Admin-configurable agent client_type allowlist

## Context

Domestic and overseas deployments need different create/install catalogs (for
example Huawei store builds should hide overseas CLIs). The full type set lives
in `model.validClientTypes`; create UIs previously treated that set (or a
hardcoded Flutter metadata list) as always available.

## Decision

- Store an optional `system_settings` row `agent_client_types` with
  `{"enabled":["…"]}`.
- Missing row or empty `enabled` means **all known types are enabled** (zero
  behavior change on upgrade).
- Admin GET/PUT `/admin/api/settings/agent-client-types` edits the list; save
  requires at least one valid `client_type`.
- User-facing create/install surfaces
  (`GET /agents/agent-api/install-guides`, REST `AgentCreate`, WS
  `create_agent` via `AgentCreateAPIForOwner`) enforce the allowlist.
- **Existing agents of a later-disabled type stay fully usable** (read, chat,
  online, edit other fields, re-submit the same client_type). Only create and
  change-to-disabled-type are rejected (`ErrAgentClientTypeDisabled` / 20020).
- Flutter metadata (`kSystemAgentClientTypes`) remains icon/label metadata, not
  the source of which types may be created.

## Alternatives

- Per-feature-gate flags per client_type: heavier ops surface for the same
  allow/deny need.
- Hardcode regional catalogs in the app: cannot be tuned without a client
  release.
- New DB table: unnecessary; `system_settings` already covers deployment-wide
  knobs.

## Consequences

- api and ws must both ship the create-time check (ws create goes through
  `AgentCreateAPIForOwner`).
- Admin and user Flutter apps must treat install-guides (or the admin settings
  API) as the enabled set for create/install lists.
- Connector `list_installable` is unchanged; the app filters that list against
  install-guides so the connector protocol stays untouched.

## Verification

- Unset setting → install-guides returns the full catalog.
- Setting `[hermes,claude]` → install-guides returns only those two; create of
  `codex` returns biz code 20020.
- Pre-existing `codex` agent remains readable/updatable after disable.
- Admin save of an unknown type is rejected.
- `go test` on systemsetting / admin/service / api/service / api/handler;
  related ws handler packages; full `flutter test` for `frontend` and `admin`.
