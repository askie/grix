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
  client type, not growing `acp`. Confirmed in practice (round2a, 2026-09-10):
  qodercli/qoderclicn/mcode/dim were promoted from `client_type: "acp"` to
  dedicated types specifically to unlock gateway relay
  (`gatewaySupportedAgentClientTypes`) and the desktop probe/picker entry
  (`agent_client_type_meta.dart`) — both explicitly out of scope for the
  generic `acp` type above. A named ACP CLI only gets these once it has its
  own client type; staying on generic `acp` means staying without them.
- The frontend renders the label `ACP Agent`; without the mapping it would fall
  back to the raw string `acp`.
- Round3 (omp, 2026-09-10) opens a second precedent alongside round2a's
  promotion-from-generic-`acp` pattern above: a client_type that starts out,
  and stays, as an independent `client_type` reusing an existing vendor
  adapter/toolbar's wire protocol verbatim, never passing through generic
  `acp` at all. `omp` drives grix-connector's `pi` adapter byte-for-byte (same
  JSONL RPC), so the connector reports `client_type: "omp"` with
  `adapterType: "pi"` — but `adapter_hint` must still be set by hand to the
  new type's own `omp/base`; it is not inherited just because the transport
  is shared. The connector's adapterType→hint fallback chain only fills in a
  default for its *own* literal, unhinted client_type (e.g. `pi` → `pi/base`),
  so a reused `adapterType` with no explicit hint silently resolves to the
  adapter it borrowed from, leaving the new client_type's own backend
  package (and any card/text differences it carries) unreachable. The
  convention this fixes going forward: `client_type` is the wire identity
  (`model.validClientTypes`, gateway relay tables, i18n, frontend picker);
  connector `adapterType` is the transport implementation, which may be
  shared across multiple client_types; `adapter_hint` is what actually
  selects the backend `agentadapter` package, and must always be set to the
  new client_type's own `<client_type>/base` regardless of which
  `adapterType` it reuses. Round4 (`deveco`, 2026-09-10) follows the exact
  same shape with a second borrowed adapter: DevEco Code is a byte-identical
  opencode fork at the connector level (`deveco serve` exposes the same
  REST+SSE OpenAPI as upstream opencode — its own `$schema` string is still
  literally `https://opencode.ai/config.json`), so grix-connector reports
  `client_type: "deveco"` with `adapterType: "opencode"` and its own explicit
  `adapter_hint: "deveco/base"`, same as `omp`→`pi` above. One consequence
  specific to this pairing, not `omp`'s: `bridge.ts`'s
  `resolveBindingChannelKey(adapterType)` keys `channel_data` by *adapterType*,
  not client_type, so a deveco session's session-binding-missing card still
  nests under `channelData["opencode"]` — `agentadapter/deveco/inbound_cards.go`
  reads that same key on purpose, not a copy-paste bug missing a rename to
  `"deveco"`. Also unlike `omp` (whose ACP-vs-generic-acp choice was never in
  question — `pi` isn't ACP at all), `deveco` was explicitly probed against the
  generic `acp` type above and rejected: `deveco acp`'s standard ACP handshake
  passed but failed 3 of the 4 route-decision criteria (session/prompt errored
  internally, available_models/modes came back in a non-standard shape,
  tool_call/request_permission were never observed), so `opencode` was the
  adapter it actually reuses, chosen because that path was verified
  end-to-end and the generic `acp` one was not.
- Round2b (grok/qwenpaw/zeroclaw, 2026-09-10) adds a third precedent: a
  generic per-session extension point for CLIs whose ACP handshake needs one
  non-standard `session/new` parameter that has no cross-vendor meaning
  (zeroclaw's `agentAlias`). Grix-connector's `AgentEntry.acp_new_session_params`
  is only accepted for ACP-based client types, must be a plain (non-array)
  JSON object, and is fail-loud rejected at config-load time if it touches a
  reserved field Grix itself owns on every `session/new`
  (`cwd`/`mcpServers`/`sessionId`/`additionalDirectories`) - a stray
  `mcpServers` key would otherwise silently drop Grix's own injected MCP
  server. This is connector-internal config surface, not a backend or
  protocol change, so it needs no `client_type`-side counterpart beyond
  registering the type itself. The same round also confirms two client
  types that stay off `gatewaySupportedAgentClientTypes` /
  `gatewayNativeProviderClientTypes` permanently, not just at launch: `grok`
  (xAI Grok Build) only authenticates via `grok login`/`XAI_API_KEY` against
  `grok.com` - no self-hosted or custom-endpoint auth method exists to relay
  through; `zeroclaw`'s `config.toml` has no `api_key_env`/`${VAR}`-style
  environment reference in its schema, so the only write path
  (`zeroclaw config set`) takes the key either as plaintext argv (visible to
  `ps`) or via a real-TTY masked prompt - every non-interactive way to
  provision it leaks the plaintext key, so it is excluded on security grounds
  rather than a missing integration.
- Round5 review (2026-09-10) surfaces a second layer that must track the
  `adapterType`-reuse pattern above but doesn't automatically: the backend's
  provider-key routing switches (`normalizeAgentSessionProviderKey` in
  `ws/handler/agent_session_bind.go`, `dispatchProviderKey` in
  `ws/agentapi/agent_invoke_dispatch_agent.go`) bucket session binding and
  rate-limit state by client_type, and any client_type missing its own
  `case` silently falls to the `"acp"` default — mixing its state with every
  other unclassified ACP client. `omp` was fixed to `"pi"` in the same round
  (it drives the `pi` adapter byte-for-byte, so it belongs in `pi`'s bucket);
  `deveco` is fixed to its own `"deveco"` bucket here too, because unlike
  `omp` it does *not* share opencode's bucket — grix-connector registers
  `deveco`'s session-history reader under the key `"deveco"`
  (`adapter/opencode/session-history.ts`), a distinct sqlite db from
  opencode's, so collapsing it into `"acp"` (or into `"opencode"`) would
  still be wrong. Both changes are safe to make outright: neither `omp` nor
  `deveco` has shipped, so there is no production `direct_key` to migrate.
  `opencode` and `deepseek` have the exact same gap (no branch, both fall to
  `"acp"`) but are **not** touched by this decision — both are released with
  live sessions, and changing their provider_key would split an existing
  session's binding/rate-limit history onto a new bucket. Fixing them is a
  pending decision that needs an explicit migration plan first, not a
  drive-by switch-statement addition.
- Round6 (2026-09-10) resolves the `opencode`/`deepseek` gap deferred above,
  with the migration plan round5 asked for. Mapping, verified against
  grix-connector source rather than guessed:
  - `opencode` → bucket `"opencode"`. Its session-history reader is
    registered under `"opencode"` in `adapter/opencode/session-history.ts`
    (`deveco`'s own reader, registered right next to it, is what keeps the
    two apart — see the round5 entry above). grix-connector's own
    `session-list.ts` `handleSyncHistoryLocalAction` independently derives
    the same value as its fallback (`agentIdOf(command)` when no explicit
    `provider_key` is supplied), which is corroborating evidence, not the
    source of truth — the source of truth is the reader registration key.
  - `deepseek` → bucket `"deepseek-harness"`, not the shorter `"deepseek"`.
    `adapter/deepseek-harness/session-history.ts` registers the same reader
    under both `"deepseek-harness"` (primary) and `"deepseek"` (alias), so
    either resolves at the connector; `"deepseek-harness"` is picked because
    it's also the connector's own `adapterType` for this client type and
    what `session-identity.ts`'s `providerKeyForAdapter()` actually reports
    back to the backend at session-open time — matching the connector's own
    stated identity beats matching an alias that happens to also work.
  - Both switches (`normalizeAgentSessionProviderKey`,
    `dispatchProviderKey`) got the same two `case` branches added, mirroring
    the `omp`/`deveco` shape from round5.
  - A connector-side bug closely tied to this same mechanism surfaced during
    review: `session-identity.ts`'s `providerKeyForAdapter()` — the value the
    connector reports in its session-open ack, which the backend's
    `agent_session_bind.go` prefers over its own computed value whenever the
    connector reports something — had no `case` for the shared `"opencode"`
    adapterType (plain opencode *and* deveco both use it). It always
    reported `"acp"`, silently overwriting whatever provider_key the backend
    had just computed for the "active" binding write. Concretely, this meant
    **round5's deveco fix never actually took effect end to end** — only the
    two independent switch unit tests passed; the real bind flow kept
    writing `"acp"` into `agent_session_bindings.provider_key` for deveco.
    Fixed by deriving the reported value from the spawned command
    (`agentIdOf(command)`, same derivation `session-list.ts`'s sync_history
    fallback already used), constrained to the two known buckets
    (`"opencode"`/`"deveco"`) rather than propagating an arbitrary command
    basename — an unrecognized command falls back to `"opencode"`.
  - Backend hardening added because of the finding above: a connector's
    open-ack `provider_key` report should not be trusted unconditionally —
    `agent_session_bind.go` now validates it against the same whitelist
    `normalizeAgentSessionProviderKey` can produce plus a length check
    (matches the column's `varchar(32)`), falling back to the backend's own
    computed value and logging a warning on anything else
    (`sanitizeReportedProviderKey`).
  - **Deployment order matters and must be followed in this direction**:
    grix-connector's fix ships first or simultaneously with the backend
    code fix, never the backend alone first. If a user's connector predates
    the `providerKeyForAdapter` fix, it keeps reporting `"acp"` for
    opencode/deveco regardless of what the backend computes — shipping only
    the backend has **zero** end-to-end effect for that user until their
    connector also upgrades. (`deepseek` is not exposed to this specific
    ordering hazard — its `providerKeyForAdapter` case already reported
    `"deepseek-harness"` correctly before round6 — but still needs the
    backend fix and the migration below for its
    `agent_session_sync_states`/`agent_native_message_imports` rows.) Running
    the *migration* before the code fix ships is a separate, also-broken
    order: any bind/dispatch in that gap recomputes provider_key with the
    unfixed logic and writes the old bucket into a fresh `direct_key`,
    which no longer matches what the migration already set — the next
    explicit re-import of that session then creates a duplicate.
  - Migration scope: unlike `omp`/`deveco` at round5 (no production data
    yet), `opencode` and `deepseek` do have live data on the old `"acp"`
    bucket, so a code-only fix isn't enough — see
    `internal/api/service/opencode_deepseek_provider_key_migration.go`
    (`RunOpencodeDeepseekProviderKeyMigration`). `deveco` is included in the
    same migration target list (added once the connector bug above was
    understood): its narrower mismatch is `agent_session_bindings.provider_key`
    stuck on `"acp"` while `agent_session_sync_states`/`sessions.direct_key`
    are already correct (they're set from the backend's already-fixed
    round5 value *before* the connector's override ever happens) — the
    migration's old-formula check on `direct_key` naturally leaves those
    already-correct rows alone and only moves the binding and
    native-message-import rows.
    Four provider_key-keyed surfaces move together per client type, each set
    wrapped in one DB transaction so a failure partway through (e.g. the
    native-message-import step hits a unique-index collision) rolls back
    that client type's work entirely instead of leaving it half-migrated:
    `agent_session_bindings.provider_key`, `agent_session_sync_states.provider_key`,
    `agent_native_message_imports.provider_key`, and `sessions.direct_key`
    (recomputed with the new bucket's `sha256(provider_key+":"+agent_session_id)`
    formula from `SessionCreateForAgentBinding`, written via `UpdateColumn`
    so the relabel doesn't bump `sessions.updated_at` and float old sessions
    to the top of the chat list). Steps 2-4 move every row still on the old
    bucket for the target client type's agents, with no `binding_id` filter
    (a live binding simply has no sync-state/native-message rows to begin
    with, so the filter would be a no-op there anyway); only step 1
    (`direct_key`) requires a non-empty `binding_id`, and it selects
    candidate bindings from *both* the old and new bucket — a binding can
    already show the new bucket if the connector fix reached a user before
    the backend fix did (see the deployment-order note above) while
    `direct_key` is still hashed with the old formula, and selecting only
    the old bucket would permanently miss that row. The old-formula
    comparison on `direct_key` itself is what actually gates which sessions
    get touched, independent of the binding's current provider_key. The
    migration function is idempotent (every step only touches rows still on
    the old bucket, or whose `direct_key` still matches the old formula's
    output) and runs only via an explicit opt-in: `cmd/migrate` takes a
    `-backfill-provider-keys` flag (default off, parsed by
    `cmd/migrate/main.go`'s `parseArgs`), so a routine deploy's plain
    `migrate config.yaml` invocation never triggers it — this only runs when
    someone deliberately passes
    `go run ./cmd/migrate -backfill-provider-keys config.yaml` after
    confirming the deployment-order requirement above. Each step logs its
    `RowsAffected` count. Rollback is
    symmetric: point `opencodeDeepseekProviderKeyTargets` back at `"acp"` and
    rerun the same four steps; no backup table is needed since no row is
    created, deleted, or renumbered — only the `provider_key` label and
    `direct_key` hash change.
  - Production impact estimate: nobody who worked this round had
    production/staging DB access, so no real row count is recorded here.
    Run before picking an execution window (column/table names per
    `internal/model`):
    ```sql
    SELECT a.agent_client_type, count(*) FROM agent_session_bindings b
      JOIN agents a ON a.id = b.agent_id
      WHERE a.agent_client_type IN ('opencode','deepseek','deveco')
        AND b.provider_key = 'acp' AND b.binding_id <> ''
      GROUP BY a.agent_client_type;

    SELECT a.agent_client_type, count(*) FROM agent_session_sync_states s
      JOIN agents a ON a.id = s.agent_id
      WHERE a.agent_client_type IN ('opencode','deepseek','deveco')
        AND s.provider_key = 'acp'
      GROUP BY a.agent_client_type;

    SELECT a.agent_client_type, count(*) FROM agent_native_message_imports m
      JOIN agents a ON a.id = m.agent_id
      WHERE a.agent_client_type IN ('opencode','deepseek','deveco')
        AND m.provider_key = 'acp'
      GROUP BY a.agent_client_type;
    ```
    Expected scope is bounded to sessions that were explicitly imported with
    history (the import gate requires `agent_session_id`), not all
    opencode/deepseek/deveco chat traffic — normal live chat never touches
    these bucket columns.

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
- `internal/ws/handler`: `TestNormalizeAgentSessionProviderKey_OmpSharesPiBucket`,
  `TestNormalizeAgentSessionProviderKey_DevecoOwnBucket` — omp buckets with pi,
  deveco gets its own bucket, an unrelated ACP client still falls to `"acp"`.
- `internal/ws/agentapi`: `TestDispatchProviderKey_OmpSharesPiBucket`,
  `TestDispatchProviderKey_DevecoOwnBucket` — same coverage for the dispatch
  path's independent switch.
- `internal/ws/handler`: `TestNormalizeAgentSessionProviderKey_OpencodeDeepseekOwnBuckets`
  — round6, opencode buckets to `"opencode"`, deepseek to `"deepseek-harness"`.
- `internal/ws/agentapi`: `TestDispatchProviderKey_OpencodeDeepseekOwnBuckets`
  — same coverage for the dispatch path's independent switch.
- `internal/api/service`: `TestRunOpencodeDeepseekProviderKeyMigration_MigratesAllFourTables`
  (all four provider_key-keyed surfaces move together, and a second run is a
  no-op), `TestRunOpencodeDeepseekProviderKeyMigration_ReimportReachesSameSession`
  (the scenario the migration exists for: re-binding the same native session
  post-migration resolves to the original aibot session instead of splitting),
  `TestRunOpencodeDeepseekProviderKeyMigration_LeavesOtherAcpAgentsAlone` (an
  unrelated client type that also defaults to `"acp"`, e.g. `qodercli`, is
  untouched), `TestRunOpencodeDeepseekProviderKeyMigration_DevecoOnlyMovesTheMismatchedRows`
  (deveco's narrower mismatch: only the binding and native-message-import
  rows move, the already-correct sync_state and direct_key are left alone),
  `TestRunOpencodeDeepseekProviderKeyMigration_FixesDirectKeyWhenBindingAlreadyOnNewBucket`
  (the deployment-order case: a binding already on the new bucket still gets
  its stale `direct_key` recomputed), `TestRunOpencodeDeepseekProviderKeyMigration_RollsBackOnPartialFailure`
  (a unique-index collision on the last step rolls back the earlier steps for
  that client type too — proves the per-client-type transaction actually
  protects against partial application).
- `internal/ws/handler`: `TestSanitizeReportedProviderKey` — an empty or
  unrecognized/oversized connector report falls back to the backend's
  computed value (with a warning logged); a recognized report still wins.
- grix-connector `tests/bridge-session-identity.test.ts`: `providerKeyForAdapter`
  cases for `adapterType=opencode` cover `opencode`/`deveco`/empty command
  plus an unrecognized command (falls back to `"opencode"`, not the raw
  derived string) and a Windows-style `.CMD`-suffixed deveco path.
- `cmd/migrate`: `TestParseArgs` — `-backfill-provider-keys` defaults to off
  and only flips on when explicitly passed, with the positional config path
  resolved correctly whether the flag precedes it, follows it (omitted, since
  Go's flag package stops parsing at the first non-flag argument), or is
  absent.
