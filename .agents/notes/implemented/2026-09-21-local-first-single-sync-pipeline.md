# Local-first single synchronization pipeline

State: implemented for durable chat state; rollout is feature-gated. Full Web
cross-tab forwarding and automatic handoff remain deferred.

## Context

Independent realtime pushes, `pull_sync`, REST summaries and history refreshes
duplicated network/SQLite/UI work and allowed screens to disagree. Message-row
maxima also could not prove that every durable event had been applied. The
chosen boundary is one synchronization owner, committed local projections and
an explicit event cursor per client installation.

## Decision

### Transactional publication and cursors

- Each user has one durable stream; each device resumes from its own local
  committed cursor. `device_sync_cursors` is diagnostic, never authority to
  force a device forward. Message rows and SharedPreferences are not cursor
  authorities, and archive history never advances the stream cursor.
- Migration `backend/migration/127_local_first_sync_v2.sql` adds
  `user_sync_heads`, append-only `user_sync_events`, `device_sync_cursors`,
  `sync_command_receipts`, entity `state_version` columns and membership
  tombstones. `syncstream.AppendTx` locks affected user heads in sorted order
  and commits business mutations, events and heads in the same PostgreSQL
  transaction. Rollback publishes none of them; Redis only wakes the drain.
- Read the head and events from primary `store.DB`, never `store.Read()` until
  an explicit replica-safe watermark contract exists. Under `READ COMMITTED`,
  freeze the head **before** querying events bounded by it. A later head read
  is a dirty hint only. An empty batch keeps `next_cursor == from_cursor`.
- Legacy `inbox_seq` is sparse: missing integers are not message loss. Its
  allocator takes sorted per-user PostgreSQL transaction locks before both
  Redis and DB allocation and holds them until commit/rollback. Redis `INCR`
  alone gives uniqueness, not commit order, and is never a published head. v1
  pull reads message, unread, cursor and call snapshots from the primary.
- v2 covers contiguous logical positions. Compound storage rows can span
  several positions and folding can remove wire events; the negotiated
  [`compound_v1` rules](2026-09-24-sync-compound-events-and-replay-folding.md)
  preserve coverage with `first_cursor`. Neither row counts nor head values
  authorize skipping unapplied positions.

### Connection and event contract

Auth advertises `sync_v2` only with `AIBOT_SYNC_V2_ENABLED=1`; exactly one
`auth_ack.active_sync` (`v1` or `v2`) is selected for a connection. The gate
defaults off in code; this is not evidence of any deployment's enabled state.

Cursor/version JSON values are decimal strings. The additive envelope is:

```text
auth: capabilities?
auth_ack: capabilities?, active_sync?
sync_resume: generation, committed_cursor, capabilities?
sync_batch: generation, from_cursor, next_cursor, head_cursor, has_more,
  events[{cursor, first_cursor?, kind, entity_type, entity_id,
          entity_version, tombstone?, command_id?, payload}],
  final_state_snapshot?{unread_by_session?}
sync_ack: generation, committed_cursor
```

Resume rejects a cursor beyond the published head. ACK must match the active
generation and exact pending `next_cursor`. One connection has at most one
unacknowledged batch; activity wakes that same drain. Pages read at most 100
stored rows, with classic expansion capped at 100 events. Heartbeat head checks
recover a lost Redis wakeup without sending idle empty batches.

The durable catalog includes message upsert/revoke, session upsert/remove,
absolute unread/read state, pin/mute and membership changes. Producers include
ordinary/agent/marketing/Egg-install messages, finalized AI streams, native
history imports, edit/revoke, history reset/delete, group lifecycle and role/
owner/name/settings changes, moderation access revocation and session/peer
pin/mute. Mutations and their events must share the business transaction.

| Legacy durable path | v2 event |
| --- | --- |
| `push_msg`, message rows in `pull_sync_resp`, `push_edit` | `message.upsert` |
| `push_revoke` | `message.revoke` tombstone |
| `session_read_sync`, `unread_sync` | `session.read_state`, `session.unread_set` |
| `session_member_changed` | `membership.changed`, optional `session.upsert` |
| `session_access_revoked` | `session.remove` with explicit reason |
| Session snapshots/summaries, pin/mute mutations | versioned session events |

On v2 connections the backend suppresses legacy durable payloads and wakes the
v2 drain; the client also ignores those pushes defensively. v1 producers remain
for old clients. `session_activity_sync` composing/viewing presence, typing,
socket state and AI token chunks stay ephemeral. Only finalized AI messages
are durable; media bytes stay on the media/CDN path.

### Local apply and read models

`ImService` owns sync lifecycle, single-flight/coalesced pull and history
demand, reconnect/retry and outbound commands. `LocalDb` owns canonical apply.
UI hot paths read local messages/sessions/search/counters and request demand
through that boundary. Entering a non-empty chat or reconnecting does not fetch
its recent history. `USE_CONVERSATION_LIST_API=true` retains the old summary
page as an explicit rollback path, rather than another default authority.

One SQLite transaction validates generation and contiguous cursor coverage,
applies entity versions/tombstones and projections, settles command receipts,
refreshes `account_counters` (total/notification/muted/mention) and commits
`sync_state`. Only after commit may local changes be published and ACK sent. Exact replay produces no second
entity/UI change. An empty terminal batch at the current cursor must still
apply its unread snapshot; an already committed older batch is a replay.

Entity `state_version` is distinct from the stream cursor: the local version
ledger stores both. Archive/bootstrap and live events use comparable server
versions; stale/unversioned projections cannot overwrite newer canonical
state. Tombstones prevent history resurrection. A version barrier must not
skip a live message body that is still missing locally. Compound message,
session and unread parts each retain their own barrier.

Unread uses absolute per-session values. Session and aggregate counter changes
commit together, so the tab and OS badges use the same local truth. Only the
terminal catch-up batch carries the authoritative unread snapshot; intermediate
session-list publication is coalesced off the ACK path. Peer read events omit
the reader's unread count. Membership payloads provide recipient/member
identity for complete private peer projections without another detail request;
group changes do not repeat the full member table per recipient, and post-update
member state is batch-read.

Access revocation removes the visible session projection but preserves history.
Explicit history reset deletes the projection and messages. Later unread,
read/pin/mute snapshots honor session tombstones and cannot recreate it.
Finalized stream placeholders are persisted even with no human recipients;
duplicate completed finalization stays idempotent.

### Bootstrap and commands

Fresh installs bootstrap through the sync owner instead of replaying the whole
retained log. Current schema is v20: the v19 sync/version/outbox/counter/lease
tables remain, with resumable progress added by
[keyset bootstrap](2026-10-11-resumable-session-bootstrap.md). It captures a
primary head before snapshot reads, persists page progress and reconciles
removed sessions only after the final page, then replays from that fixed head.
Migration 129/backend support must precede the new client. Bounded
[recent message bodies](2026-09-25-sync-v2-bootstrap-recent-messages.md) attach
only to the first keyset page (up to 20 sessions × 30 messages). Empty-chat and
upward-pagination history demand uses the same version/tombstone reducer and
does not move the durable cursor.

v2 send, revoke, read, history reset and session/peer pin/mute use a durable
outbox with reused command IDs. Optimistic state and its command commit
together; pre-v19 `sending` rows are backfilled and send ACK reconciliation is
transactional. Backend send uses `client_msg_id`; read/edit/revoke/history/
pin/mute mutations claim `command_id` in their business transaction. Committed
retries, including no-op commands with durable receipts, do not append another
mutation or increment versions. This does not assert every client edit uses
the outbox.

Outbox flush is serialized. Network, 408, 429 and 5xx failures retain commands
for backoff, bounded by 20 attempts in current code; exhaustion uses the same
rejection path. Terminal HTTP/business failures atomically remove the command and
roll back optimistic state only if its recorded base version is still current;
a late failure cannot overwrite a newer authoritative event. Poison commands
must not block later work. Gate rollback to v1 drains persisted REST mutations
using their committed response as receipt while legacy send/read/history keep
their existing owners. Confirmed commands are removed; only one compact
history-reset receipt per locally deleted session prevents reconnect resends.

### Web scope and rollout

Web has one writer per account/database: renew the exclusive lease every 5s,
expire after 15s. A losing tab is read-only with a reload affordance and opens
no writer socket; lease loss disconnects the writer. Independent stores are
independent client instances/cursors. `BroadcastChannel` command/commit
forwarding, notification-loss recovery and automatic handoff need a separate
review; lease expiry plus reload/reconnect is not automatic handoff.

Deploy migration 127 and additive backend support to every node with the gate
off, then capable clients (fallback v1), then internal/percentage rollout.
Message, unread/membership and summary/settings producer slices require their
own coverage and acceptance evidence. Retain v1 writers until the minimum
supported client version no longer needs them. To roll back, disable
`AIBOT_SYNC_V2_ENABLED` and recycle sockets; keep additive tables/columns.
Compound rows impose an additional permanent reader constraint in the linked
compound decision; turning a feature flag off does not erase stored rows.

Numeric acceptance gates for the same scripted 10-minute group workload:

- Zero warm-navigation history/summary requests; zero extra idle sync work
  beyond heartbeat; at most one pending batch per client database.
- Zero changed SQLite rows/UI publications on exact replay; one transaction
  per durable batch; transfer durable payloads through one active sync lane.
- Healthy-socket foreground cursor lag p95 <2s / p99 <10s; 100-entry batch apply
  p95 <100ms on the supported baseline iOS device.
- At least 50% fewer chat SQLite writes/network wakeups in matched Instruments
  traces; no increase above 0.1% in retry/reconnect loops, crash-free-session
  regression or unread divergence reports at each stage.

Any invariant violation, permanent cursor skip or threshold regression disables
the relevant gate. History-page reconciliation is not a hidden rollout repair.
Actual telemetry acceptance and checkpoint-aware log retention remain rollout
work, not proven by unit tests. Retention must never invalidate a supported
client cursor; unbounded append-only production storage is not the final policy.

## Alternatives

- Keep push/pull/page repair in parallel: rejected; idempotent writes still
  consume network, decode, transaction and UI work and obscure cursor safety.
- Use SQLite only as a server-query cache: rejected; weak offline behavior
  and page/network lifecycle coupling preserve divergent views.
- Replay all history on install: rejected for unbounded startup cost.
- Persist AI token chunks: rejected for write/render frequency.
- Implement full Web leadership now: deferred; single-writer exclusion solves
  the immediate correctness boundary without promising cross-tab freshness.

## Consequences

Local projections make screens deterministic and useful offline, while the
reducer and protocol become critical infrastructure. Compatibility temporarily
costs two implementations, but each connection activates one. Crash before
commit replays; crash after commit/before ACK replays harmlessly; SQLite failure
must neither advance nor ACK. Version barriers protect races with history,
and per-device cursors prevent one device consuming for another.

## Verification

Current enforcement points (paths from repository root):

- `backend/internal/syncstream/{stream.go,message_events.go,fold/}` and
  `backend/internal/ws/handler/sync_v2{,_test}.go`: transactional heads,
  frozen-head drain, generation/exact ACK, compound expansion and heartbeat.
- `backend/internal/api/service/session_service_list{,_recent}.go` and
  `backend/internal/api/service/session_service_bootstrap_test.go`: primary
  bootstrap, visibility and bounds.
- `frontend/lib/data/providers/{im_service_sync_v2.dart,local_db_sync_repository.dart,local_db_lifecycle.dart}`:
  commit-before-publish/ACK, entity barriers, outbox and lease; tests in
  `frontend/test/data/providers/{im_service_sync_v2_protocol_test.dart,local_db_sync_v2_test.dart}`.
- `backend/internal/syncstream/stream_pg_test.go` and
  `backend/internal/pkg/inboxseq/inbox_seq_pg_test.go`: real-PostgreSQL
  publication/rollback ordering. Migration discovery alone does not prove SQL
  execution; deployment still requires the additive migration chain.

Regression coverage must retain multi-batch catch-up, concurrent triggers,
crash/replay, zero-progress terminal snapshots, stale archive/edit/revoke,
bootstrap races, two devices, outbox restart/terminal failure, local-only warm
navigation, v1 fallback and Web lease exclusion/release/expiry. Operational
rollout metrics and full Web handoff are separate from automated coverage.
