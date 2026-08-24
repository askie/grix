# Session history sync is forwarded from the API process to the ws node

## Context

Provider-native session history (a connector-side CLI transcript imported after `agent_session_bind`) is pulled through a `session_control` / `sync_history` local action, which needs the agent websocket `Manager`. Agents connect to `aibot-ws`; the REST process `aibot-api` never constructs a `Manager`, so `wsagentapi.GetGlobal()` is nil there. `GET /v1/messages/history` triggered the sync from `aibot-api` and always failed with `agent not connected`, so a session bound from the mobile client never showed its CLI history.

## Decision

`orchestrator.SyncBoundSessionHistory` keeps running the sync locally when a `Manager` exists. When it does not, it forwards the whole-session sync over Redis: `wsagentapi.RemoteSyncBoundSessionHistory` publishes `_agent_api_session_history_sync` to `chan:<node>` of the node that owns the agent route (owner route first, primary route fallback, first routable bound agent wins), subscribes to a per-request reply channel `chan:session_history_sync:<correlation>` before publishing, and waits up to five minutes. The ws node runs the sync through the handler registered by `ws.Server` (`SetSessionHistorySyncHandler(historysync.SyncBoundSessionHistory)`) inside the Manager background group and publishes `{correlation_id, imported, error_msg}` back. A node without a Manager answers `agent not connected`; a node without a handler answers `session history sync handler unavailable`.

## Alternatives

- Run the sync on the ws node right after `agent_session_bind`: rejected as the only path because the REST history fetch still needs a result and retry semantics, and the bind reply must not wait for a multi-page import.
- Give `aibot-api` its own agent `Manager` or move the history endpoint to ws: far larger blast radius for one call path.
- Reuse the per-page `local_action` forward from the API process: the API process has no node channel subscription, so the per-action reply could never return; a whole-session RPC with its own reply channel avoids that.

## Consequences

- The Redis node channel gains one command; both `aibot-api` and `aibot-ws` must be deployed together for the fix to take effect.
- The five-minute remote timeout bounds the background goroutine started by the REST handler; the handler itself still only waits `nativeHistorySyncWait` before returning current data.
- Errors from the ws side are surfaced by message, not by typed error value, across the process boundary.

## Verification

- `internal/ws/agentapi/session_history_sync_remote_test.go` drives a fake ws node over miniredis: success, remote error with partial import, no route, no Redis, node without Manager, node without handler, silent node timeout, foreign command ignored.
- `internal/agentsync/orchestrator/sync_test.go::TestSyncBoundSessionHistoryForwardsWhenNoLocalManager` proves the API-side path forwards with every bound agent as a routing candidate instead of failing offline.
