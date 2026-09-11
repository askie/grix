# agent_connection_logs disconnect reconciliation

## Context

`agent_connection_logs` (migration `094_agent_connection_security.sql`) writes one
row per successful agent WS handshake and expects the disconnect path to fill in
`disconnected_at` / `disconnect_reason`. In production, `disconnected_at IS NULL`
("still online") had accumulated to 1519 rows across two ws nodes over ~2 months,
against ~221 actually-online agents; one agent alone held 67 open rows.

`finalizeAgentConnection` (`backend/internal/ws/agentapi/conn_security.go`) already
runs on every reachable disconnect path: normal close and kick
(`ws_gateway.go` `unregister`), the connection-superseded/auth-rejected branch, and
graceful shutdown (`Manager.Shutdown` closes every tracked connection, which drives
`ServeWS`'s deferred `unregister` before `Manager.Shutdown` returns — verified by
`waitBackground`'s defer ordering and covered by
`TestManagerShutdownFinalizesConnectionLog`). None of those paths run when the
process is killed before it can execute its deferred cleanup — SIGKILL, OOM-kill,
or a rolling restart that outruns `shutdownWait`. That is the dominant leak source
and matches an already-established pattern in this codebase for the same failure
class: `ws.StartRouteJanitor` sweeps `im:ws:route` entries left behind when a
process is killed, and `Manager.ReconcileLeakedSessionStatesOnStartup` heals
`session_agent_state` rows left non-terminal by a prior ws crash.

## Decision

Add `Manager.ReconcileStaleConnectionLogsOnStartup`, wired into
`internal/ws/server.go` the same way as `ReconcileLeakedSessionStatesOnStartup`
(`GoBackground`, async, non-blocking). On startup it closes rows where
`node_id = <this node's node_id>` and `disconnected_at IS NULL` and
`connected_at < <reconcile call time>`, setting `disconnect_reason =
"startup_reconcile"`.

Two constraints, both load-bearing:

- **Filter by `node_id` only.** During a rolling deploy the old and new pod both
  run under distinct node IDs and can be online at the same time. Reconciling by
  any broader condition (e.g. "all rows older than N minutes") risks closing a
  row that belongs to the *other* node's still-live connection.
- **`connected_at` must be older than the reconcile call's own timestamp.** This
  node's own fresh connections (recorded by `recordAgentConnection` after this
  process starts accepting traffic) always have a later `connected_at`, so the
  reconcile pass can never race a connection this same process instance is about
  to serve.

No change was needed for the graceful-shutdown or connection-superseded paths —
both already finalize correctly; they only lacked test coverage at the
integration level (see Verification).

## Alternatives

- **Periodic janitor instead of startup-only reconcile** (mirroring
  `StartRouteJanitor`'s periodic sweep): rejected for this table. A leaked row is
  only "resolved" here in the sense of being marked closed, not undone — a
  periodic sweep would delay closing it, but startup is already the correct and
  sufficient trigger, since `node_id` only changes identity across restarts.
- **Blanket TTL-based reconcile** ("close anything open longer than a few
  hours"), independent of node_id: rejected — cannot distinguish "the other node
  in a rolling deploy hasn't restarted yet" from "this node crashed", and would
  reintroduce the exact misclose risk the current design avoids.

## Consequences

- New disconnect_reason value `startup_reconcile` appears in the table going
  forward, alongside the existing `closed` / `connection_superseded` /
  `replaced_by_new_connection`. Nothing reads `disconnect_reason` for logic today
  (checked: the only consumers of this table are the admin/owner connection-log
  listing endpoints and `frontend/lib/modules/ai/models/agent_conn_security_model.dart`'s
  `isOnline = disconnectedAt == null` display, and
  `inactive_agent_users_service.go`, which only reads `connected_at`, never
  `disconnected_at`). The admin dashboard's online-agent count
  (`countOnlineAgentsFromRedis`) is Redis-based and untouched by this table.
- The existing ~1519-row backlog is not touched by this code change — the
  companion one-time cleanup is a manually-reviewed, manually-run SQL script
  (`backend/scripts/adhoc/2026-09-12-agent-connection-logs-cleanup.sql`),
  deliberately kept outside `backend/migration/` so `cmd/migrate` never applies it
  automatically. Its per-`(agent_id, owner_id)` "keep only the latest open row"
  rule is safe by construction: the ws layer allows only one live connection per
  `(agent_id, owner_id)` at a time, so a genuinely-online pair's real connection
  is always the most recent row in that group. Any residual open row the script
  conservatively preserves (because the agent is actually offline but happens to
  own the most-recent row in its group) self-heals the next time that specific
  node restarts, via the new startup reconcile.

## Verification

- `backend/internal/ws/agentapi/conn_log_lifecycle_test.go`:
  - `TestManagerShutdownFinalizesConnectionLog` — graceful shutdown finalizes the
    node's own tracked connection through the real `ServeWS` lifecycle (not just
    a direct call to `finalizeAgentConnection`).
  - `TestSupersedingConnectionFinalizesOldConnectionLog` — a new connection
    replacing an old one via `attachConn` finalizes the old row and leaves the
    new row open.
  - `TestReconcileStaleConnectionLogsOnStartup` — closes only same-`node_id`,
    pre-cutoff, still-open rows; leaves another node's rows, post-cutoff rows,
    and already-closed rows untouched.
- `go test ./backend/internal/ws/...` — full package, no regressions.
- `go build ./...` — backend builds clean.
