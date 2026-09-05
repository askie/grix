# Generic `acp` client type

## Context

`backend/internal/agentadapter/acp` (`acp/base`, family `acp`) was registered in
`internal/ws/server.go`, but `acp` was missing from `model.validClientTypes`.
Agent creation rejected it in `agent_service_validation.go` and the WebSocket
handshake answered `10003 invalid client_type` in `ws/agentapi/ws_gateway.go`,
so the adapter was unreachable code.

Every other agent reaches the platform through a vendor client type whose CLI,
spawn command, slash commands and quota surface the backend knows by name.
That model does not extend to an arbitrary CLI that happens to implement the
Agent Client Protocol: there is no vendor to name.

## Decision

Open `acp` as a vendor-neutral client type for any ACP-compatible CLI. The owner
writes `client_type: "acp"` plus `command` / `args` in the grix-connector agent
entry; the connector spawns that executable and reports `client_type: "acp"` with
`adapter_hint: "acp/base"`.

Scope, deliberately narrow:

- `acp` counts as proprietary for group dispatch — it drives a coding CLI, so an
  unmentioned group message must not start a run on it.
- The toolbar (`agenttoolbar/agents/acp`) carries only what the protocol itself
  guarantees: stop output, session control, and model/mode selectors that render
  only once the connector reports `available_models` / `available_modes`.
- The install guide reuses the shared connector task in every app language and
  only swaps in the config entry carrying `command` / `args`.
- Session-control replies say "ACP" and read only `acp_session_id` /
  `acpSessionId`.

Explicitly out of scope, because a generic CLI gives the backend nothing to key
on: gateway relay (`gatewaySupportedAgentClientTypes`), rate-limit auto-fetch,
egg skill-package install targets (there is no known skill directory), the
static slash-command catalog in `agentslashcmd`, and a desktop probe entry in
`agent_client_type_meta.dart`. No database migration, no protocol field, no
ACP-specific card.

These gaps are closed by decision (2026-09-05), not deferred. The connector side
matches: for `client_type: "acp"` skill discovery falls back to the Gemini
layout (`~/.gemini/skills`, shared with a real Gemini agent on the same
machine) and `get_session_usage` is not declared because there is no usage
parser for an unknown CLI. A CLI that needs any of these gets its own
`client_type`; `acp` does not grow per-vendor branches.

## Alternatives

- **A vendor client type per ACP CLI.** Rejected: each new CLI would need a Go
  constant, adapter, toolbar package, install guide and frontend label, for a
  set of behaviors identical across all of them.
- **Reuse an existing ACP-backed type (qwen, reasonix, kiro).** Rejected: those
  carry vendor-specific spawn commands, mode whitelists and quota surfaces that
  would silently misdescribe a different CLI.
- **Give `acp` the union of the vendor toolbars.** Rejected: every item would be
  a control the backend cannot know the CLI supports, leaving permanently
  disabled entries in the owner's UI.

## Consequences

- Existing client types and adapter selection are untouched: `acp` is its own
  adapter family, so the other 18 adapters keep resolving as before.
- An ACP CLI that supports neither `set_model` nor `set_mode` simply shows a
  toolbar without those selectors, rather than a disabled control.
- Adding a vendor-specific capability later means promoting that CLI to its own
  client type, not growing `acp`.
- The frontend renders the label `ACP Agent`; without the mapping it would fall
  back to the raw string `acp`.

## Verification

- `internal/model`: `TestACPAgentClientType` — `acp` is a valid client type and
  dispatches mention-only in groups.
- `internal/agentadapter`: `TestSelectByMeta_SelectsACPBaseForGenericACPClient` —
  both the `adapter_hint` and family-only paths select `acp/base` undegraded.
- `internal/ws/agentapi`: `TestServeWS_AuthAckIncludesACPAdapterContract` — the
  handshake succeeds and `auth_ack` returns `adapter_id: acp/base`.
- `internal/agenttoolbar`: `TestACPPackageBuild_*` / `TestACPPackageHandleAction_*`
  — selectors follow reported meta, no vendor items, undeclared local actions are
  rejected without dispatch.
- `internal/api/service`: the existing install-guide language-matrix tests cover
  the new `acp` entry in all eleven app languages.
