# Sync v2 Bootstrap Attaches Recent Message Bodies

## Context

In sync v2, a first-connecting client calls `GET /v1/sessions/list?sync_head=1`, receives a session snapshot plus `sync_head_cursor`, and jumps its durable cursor straight to the head; `message.upsert` events before the head are never replayed. The snapshot's `last_msg` is only a 60-rune preview, so the snapshot coverage was structurally smaller than the cursor skip range: on a new device, history messages produced before bootstrap (e.g. the first message of a fresh 3-5-message session) never landed in the local database, and the conversation rendered with its head missing.

## Decision

Only when the request carries `sync_head=1`, each of the top 20 sessions in list order (pinned first, then `last_active_at DESC`) gains an optional `recent_messages` field: up to 30 full message objects per session, shaped exactly like `GET /v1/messages/history` entries (same `model.Message` JSON, same media re-signing at egress), ordered by `msg_id DESC`. Visibility filtering is identical to the history endpoint: exclude `is_deleted` and `msg_type=4` stream placeholders, apply the per-user `session_history_resets.deleted_before` cutoff and group-member `joined_at` truncation, and apply the `visible_to` filter only on PostgreSQL. Cursor semantics, `sync_batch`, and resume are unchanged. The bounds are named constants `bootstrapRecentMessagesSessionLimit = 20` and `bootstrapRecentMessagesPerSession = 30` in `backend/internal/api/service/session_service_list_recent.go`; one window-function SQL (`ROW_NUMBER() PARTITION BY session_id ORDER BY msg_id DESC`) serves all supported dialects with no N+1.

## Alternatives

- Keep summaries only and let clients backfill via `/v1/messages/history` per session: rejected because it adds one request per session to every cold start and leaves a window where the local store is incomplete.
- Replay pre-head `message.upsert` events instead of attaching bodies: rejected because it changes cursor semantics and re-opens the replay volume bootstrap was designed to avoid.
- Per-session LATERAL queries on PostgreSQL (as `loadVisibleLastMsgSummaryMap` does for LIMIT 1): not chosen; the window-function form already used for the non-Postgres dialect covers K-per-session in one statement and stays index-bound by `idx_msg_session_time (session_id, msg_id DESC)`.

## Consequences

Bootstrap responses grow by up to 20 × 30 full message bodies; the top-20 cap is the load control and must not be widened without revisiting this note. Older clients ignore the unknown field. Attachment is best-effort: if the extra query fails, the server logs a warning and still returns the session list without `recent_messages` (mirroring the client's own try/catch), because failing the whole bootstrap over a purely additive query would trap first-connecting devices in a reconnect loop. Any change to history-endpoint visibility (`buildVisibleSessionMessageQuery`) must be mirrored in `bootstrapRecentMessagesSQL`, or new devices will render messages the chat page hides (or vice versa).

## Verification

`backend/internal/api/service/session_service_list_recent_test.go` covers attachment only under `sync_head=1`, full-body content, `msg_id DESC` order, deleted/stream-placeholder exclusion, history-cutoff filtering, the per-session cap of 30, the session cap of 20, the PostgreSQL-only `visible_to` clause, and graceful degradation (list still returned, no `recent_messages`) when the attach query fails. `go test ./internal/api/...` passes.
