# Generic `acp` client type

## Context

A vendor-neutral ACP-speaking CLI needs a valid platform identity without
pretending to be a vendor whose spawn command, quota parser and toolbar are
known. Shared transport does not imply shared session identity or capabilities.

## Decision

`client_type: "acp"` is valid, proprietary/mention-only for group dispatch, and
selects `acp/base`. The connector entry supplies `command` / `args`; the install
guide reuses the shared task in every app language. Flutter labels it
`ACP Agent`. Vendor behavior belongs in a dedicated client type.

The current directory and binding policy is
[static configured cwd](2026-09-24-generic-acp-static-working-directory.md).
Generic ACP has no owner session-control/binding gate or history-list button.
The protocol toolbar offers stop-output and reported model/mode controls;
current code also displays reported skills and agent-declared custom items.
Thus it can be visible while idle when declared items exist. Undeclared local
actions fail closed; custom selectors validate the current snapshot, online
state and absence of an active run before sending owner command text.

Generic ACP has no gateway relay, automatic quota fetch, known Egg install
target, static vendor slash catalog or desktop probe entry. An unknown CLI has
no reliable vendor layout/parser; do not add per-vendor branches to `acp`.
The connector integration's Gemini-layout skill fallback and absence of a
`get_session_usage` parser are separate from platform Egg-install support.

### Client identity, transport and provider keys

These are different contracts:

- `client_type` is platform identity (validation, gateway policy, i18n, picker).
- Connector `adapterType` is transport and may be reused. Explicit
  `adapter_hint: "<client_type>/base"` selects the new backend package;
  borrowing transport must not silently select the borrowed backend adapter.
- `provider_key` partitions binding, history import and rate-limit state.
  Both `normalizeAgentSessionProviderKey` and `dispatchProviderKey` must agree
  with the connector's history-reader registration and open ACK.

| Client | Connector transport | Backend hint | Provider bucket |
| --- | --- | --- | --- |
| `acp` | ACP | `acp/base` | `acp` fallback |
| `omp` | `pi` JSONL | `omp/base` | `pi` |
| `deveco` | `opencode` REST/SSE | `deveco/base` | `deveco` |
| `opencode` | `opencode` REST/SSE | `opencode/base` | `opencode` |
| `deepseek` | `deepseek-harness` | `deepseek/base` | `deepseek-harness` |

Deveco has its own history DB despite shared transport. Its binding-card
`channel_data` remains under `opencode`, keyed by adapter type. The generic
ACP route was rejected for that CLI after an incomplete/nonstandard ACP
session probe; transport choice needs verified behavior, not handshake alone.
QoderCLI/QoderCLICN/MCode/Dim are dedicated identities for their vendor
capabilities, rather than exceptions in generic ACP.

Connector `acp_new_session_params` remains an ACP-only plain JSON object;
reject reserved `cwd`, `mcpServers`, `sessionId`, `additionalDirectories` at
config load so an extension cannot replace Grix's injected server/session data.
It is connector configuration, not a new backend protocol field.

Grok and ZeroClaw stay excluded from gateway relay deliberately: Grok's probed
auth had no custom endpoint, while ZeroClaw's available key-write paths exposed
plaintext argv or required a real TTY. Do not enable relay merely because the
runtime transport is ACP; re-evaluate the auth/security boundary first.

### Existing provider-key data and rollout

The opencode/deepseek/deveco mapping fixes are implemented, not pending work.
An open ACK's reported key wins only if recognized and at most 32 bytes;
`sanitizeReportedProviderKey` otherwise retains the backend-computed value
(invalid non-empty reports log a warning). Whitelisted `acp` is still accepted,
so an old opencode/deveco connector reporting it can defeat a backend-only fix.

Ship the connector's `providerKeyForAdapter` fix first or with the backend,
then run any explicitly authorized data backfill. Running migration before
fixed writers can create new mismatched direct keys and duplicate imports.
The connector's shared opencode transport derives only the known
`opencode`/`deveco` buckets from its command, falling back to `opencode` for an
unknown command; DeepSeek uses primary `deepseek-harness`, not alias `deepseek`.

`backend/internal/api/service/opencode_deepseek_provider_key_migration.go`
provides the opt-in `RunOpencodeDeepseekProviderKeyMigration`. For each target
client type one transaction moves four surfaces together:

1. `sessions.direct_key`, only when a non-empty binding and the old `acp`
   formula match. Candidate bindings include **both** old/new buckets, since
   a fixed connector can update the binding before its direct key is healed.
2. `agent_session_bindings.provider_key`.
3. `agent_session_sync_states.provider_key` (import progress).
4. `agent_native_message_imports.provider_key` (deduplication).

The last three relabel all old-bucket rows for target agents without a
`binding_id` filter. Live bindings have no native-import rows; reconstructing
their timestamp-based direct key would be unsafe. Direct keys use the existing
owner/agent prefix and 32-hex-character truncated SHA-256 of `provider_key:binding_id`;
`UpdateColumn` preserves session timestamps/list order. An already-correct
Deveco direct key is untouched. A collision rolls back that client type;
re-running after success is a no-op. Unrelated ACP types are excluded.

The CLI flag defaults off; from `backend`, the operator-only command is
`go run ./cmd/migrate -backfill-provider-keys config.yaml` (flag before the
positional config). Routine migration does not run it. No production/staging
completion or row counts are established by this note: inspect affected
bindings, sync states and native imports joined to target agents, plus stale
direct-key formulas even when bindings are already on new buckets, before
choosing a window. A reverse migration needs its own reviewed predicates;
the helper hardcodes the old bucket as `acp`, so changing target values alone
is not a verified rollback procedure.

## Alternatives

- A vendor type for every unknown CLI: unnecessary registration/UX duplication
  where only generic ACP is known. Promote when capabilities require it.
- Reuse another vendor's identity or toolbar union: misstates spawn, mode and
  quota support and exposes controls the CLI may not implement.
- Infer backend adapter/provider bucket from reused transport: rejected;
  Deveco and OMP show why transport reuse and storage identity differ.

## Consequences

Generic ACP stays small; selectors follow reported metadata. Vendor additions
must audit validation, adapter hint, capability lists and both provider-key
switches, including the connector report and stored-data migration. A backend
switch unit test alone cannot establish correct end-to-end binding identity.

## Verification

Paths from repository root:

- `backend/internal/model/model_test.go` and
  `backend/internal/agentadapter/selector_test.go`: generic ACP validity,
  proprietary dispatch and adapter selection.
- `backend/internal/agenttoolbar/{acp_package_test.go,acp_custom_toolbar_test.go}`:
  reported selectors/custom items, no session control, invalid/stale actions.
- `backend/internal/ws/handler/agent_session_bind_provider_key_test.go` and
  `backend/internal/ws/agentapi/agent_invoke_dispatch_agent_test.go`: matching
  OMP/Deveco/OpenCode/DeepSeek buckets and report sanitization.
- `backend/internal/api/service/opencode_deepseek_provider_key_migration_test.go`:
  four-surface migration, stable re-import, unrelated types, already-new
  bindings with stale direct keys, idempotence and transaction rollback.
- `backend/cmd/migrate/main_test.go`: explicit opt-in parsing;
  `backend/internal/api/service/gateway_service_test.go`: Grok/ZeroClaw denial.
- Connector integration checks remain required for `providerKeyForAdapter`,
  reserved ACP parameters and borrowed transport/card keys. Their source and
  tests are in the separate connector repository; this note does not certify
  its installed version or a production backfill.
