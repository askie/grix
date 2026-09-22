# Local-first single synchronization pipeline

## Status

- State: accepted and implemented for the reviewed scope; rollout remains feature-gated, and full Web cross-tab forwarding/automatic handoff is explicitly deferred
- Owner: Grix client and WebSocket synchronization
- Scope: durable chat messages, conversation summaries, unread state, and client-side projections
- Explicitly out of scope: media bytes, authentication, ephemeral typing/presence, and AI token-by-token streaming

## Context

The Flutter client currently has the right local building blocks but does not
have one authoritative data flow. Durable chat state can arrive through
realtime WebSocket commands, `pull_sync`, session snapshot/sync REST calls,
conversation-summary REST calls, and message-history REST calls. Some of those
paths write SQLite first, while others update in-memory UI projections directly.

This creates four classes of failure:

1. the same remote state is fetched and decoded more than once;
2. idempotent database writes still cause UI work and device wakeups;
3. cursors derived from message rows do not represent all processed events;
4. screens can disagree because SQLite, service-owned reactive lists, and
   controller-owned server summaries are concurrent sources of truth.

The desired model is local-first: every durable server change is consumed by a
single synchronization owner, committed to the client database, and only then
published as a local change. Chat pages, the conversation list, search, tab
badges, and the operating-system badge read local projections only.

## Decision

### Review adjudication

The independent architecture review returned **conditionally approved**. The
following findings are accepted and are binding on implementation:

- **Accepted, P0:** Redis sequence allocation currently occurs outside the
  PostgreSQL transaction. Allocation order therefore does not prove commit
  order, and `MAX(inbox_seq)` is not a safe published watermark.
- **Accepted:** every local canonical entity needs an enforceable server/event
  version; prose alone is insufficient.
- **Accepted:** the synchronization stream must read the primary database until
  a replica-safe watermark contract exists.
- **Accepted:** existing durable WebSocket commands need an explicit migration
  map and cannot be converted in one undifferentiated release.
- **Accepted:** rollout gates need numeric acceptance and rollback thresholds.
- **Accepted with scope reduction:** full Web multi-tab leader handoff is
  deferred. The first Web milestone enforces a single writer and treats any
  additional independently stored tab as another client instance.

No review finding was rejected. Full Web leader election was not judged
incorrect; it was deferred because it is greenfield work that is unnecessary
for the immediate duplicate-fetch and energy problem.

The post-implementation independent review on 2026-09-22 found one P0, two
P1, five P2, and four P3 items. The implementation owner independently
reproduced and accepted all correctness findings:

- the P0 drain race is fixed by reading the primary publication head first,
  bounding the following event query by that frozen head, and never advancing
  an empty batch. A second head read is only a dirty hint and cannot advance
  the cursor;
- bootstrap now returns the transactional `sync_head_cursor` from the primary
  and stores it as the local committed cursor with the snapshot, so a fresh
  client does not replay the complete retained log;
- HTTP/business-terminal Outbox failures are separated from transient network,
  rate-limit, and 5xx failures. Terminal commands are removed and their
  optimistic projection is rolled back in one SQLite transaction, allowing
  subsequent commands to continue. Each optimistic command records its base
  entity version, and rollback is skipped when a newer authoritative event has
  already advanced that version, preventing a late HTTP failure from
  overwriting newer synchronized state;
- the client now requires every batch cursor to be contiguous. It distinguishes
  a replayed already-committed batch from a zero-progress batch at the current
  cursor, so the latter still applies its final unread snapshot instead of
  silently discarding reconciliation state;
- heartbeat checks the primary head as a low-frequency fallback for a lost
  Redis wake-up; no-op read commands append a durable command receipt event;
- group membership events no longer repeat the full member table per recipient,
  post-update member state is batch-read instead of N+1 queried, session-list
  refreshes are coalesced off the ACK-critical path, Web followers show a
  read-only banner, peer read events omit the reader's unread count, and
  `session.remove` carries an explicit reason;
- finalized stream placeholders are updated even when a session has no human
  recipients, while completed duplicate finalization remains idempotent.

The remaining performance observations are treated as rollout/retention work,
not correctness exceptions: a large-group message still intentionally emits
separate message, session, and unread entity events, and the append-only log
needs a checkpoint-aware retention policy before unbounded production use.

### Previously implemented Milestone 1 slice

The first implementation slice deliberately stays wire-compatible and removes
the overlapping hot paths responsible for the observed energy regression:

- `pull_sync` is single-flight and request-sequenced; concurrent resume,
  realtime-gap, manual, and retry triggers collapse into one queued follow-up;
- exact message/edit replays return an explicit unchanged result, advance the
  cursor when persistence succeeded, and publish no second UI event;
- identical archive requests share one in-flight future;
- entering a non-empty chat and reconnecting no longer request the latest
  session-history page; explicit upward archive pagination remains available;
- the conversation page defaults to the local session projection. The old
  server-summary page is retained only as the
  `USE_CONVERSATION_LIST_API=true` rollback switch;
- current `pull_sync` reads messages, unread snapshots, cursor snapshots, and
  call snapshots from the primary database;
- every current `inbox_seq` allocation path now takes sorted per-user
  transaction locks even on the Redis fast path and holds them until the
  surrounding business transaction commits.

The last item is the immediate P0 remediation. Redis `INCR` guarantees unique
numbers but not database commit order; the transaction lock supplies commit
serialization. Rolled-back allocations may leave harmless sparse gaps, while
two committed events for one user can no longer become visible in reverse
sequence order. The v2 publication-head design below remains the explicit,
inspectable protocol authority for the expanded event catalog.

### Backend Milestone 2/3/4 implementation (2026-09-22)

The backend implements the server portion of the expanded design.

- Migration `127_local_first_sync_v2.sql` additively creates
  `user_sync_heads`, append-only `user_sync_events`, per-device
  `device_sync_cursors`, and `sync_command_receipts`. It adds
  `state_version` to messages, sessions, memberships, history-reset rows, and
  peer pin/mute rows, plus a membership tombstone marker. Existing v1 tables
  are retained.
- `syncstream.AppendTx` seeds and locks affected user heads in sorted order,
  allocates event cursors, inserts events, and advances published heads in the
  caller's PostgreSQL business transaction. A rollback exposes neither the
  business mutation, event, nor head. Redis remains a wakeup transport only.
- The server advertises `sync_v2` only when `AIBOT_SYNC_V2_ENABLED=1`; auth
  selects exactly one `active_sync` value (`v1` or `v2`) for the lifetime of a
  connection. v2 batches read `store.DB` (the primary), contain at most 100
  events, freeze the published head before reading rows bounded by that head
  under PostgreSQL `READ COMMITTED`, and permit only one unacknowledged batch
  per connection. An empty result never advances a cursor.
- Durable producers now append catalog events in the same transaction for
  ordinary/agent/marketing/Egg-install message creation, finalized AI streams,
  native agent-history imports, edit, revoke, absolute unread/read state,
  per-user history reset/delete, group creation/join/add/remove/leave/role/
  owner transfer/rename/nickname/convert/dissolve, moderation access
  revocation, speaking and agent-receive settings, and session/peer pin and
  mute state.
- Sending already uses durable `client_msg_id` receipts. Read, edit, revoke,
  history reset/delete, session/peer pin, and session/peer mute accept a
  `command_id` and claim it in `sync_command_receipts` within the mutation
  transaction. A committed retry, including a committed no-op, does not
  increment entity versions or append another event.
- v1 inbox rows and legacy realtime/offline producers remain for old clients.
  On a v2 connection, the connection boundary suppresses only legacy durable
  payloads and turns them into a wakeup for the single v2 drain. Ephemeral
  typing/viewing activity and AI token streaming continue on their existing
  lane and are not persisted in `user_sync_events`.
- This suppression is the backend Milestone 4 narrowing performed during the
  compatibility window. The v1 writers are intentionally not deleted while
  supported clients still require them.

### Flutter/Web client implementation (2026-09-22)

The client side of the reviewed scope is also implemented:

- Local database schema v19 adds `sync_state`, `sync_entity_versions`,
  `outbox`, `account_counters`, and `sync_writer_lease`. Upgrade from every
  supported historical schema is guarded so v19-only indexes are not created
  before their tables.
- `sync_v2` auth negotiation is additive. A server-selected v2 connection uses
  only resume/batch/ACK for durable downstream state and defensively ignores
  legacy durable pushes; a v1 selection retains the previous pull/push path.
- A single SQLite transaction validates generation/cursors, applies entity
  versions and tombstones, updates message/session/unread projections,
  acknowledges command receipts, refreshes counters, and advances the local
  cursor. Local change events and protocol ACKs occur only after commit.
- The first v2 use performs a bounded, complete session bootstrap through the
  synchronization owner. Snapshot session/member versions seed the same
  version barrier used by realtime events, and the snapshot response carries a
  primary transactional event head that becomes the initial committed cursor;
  warm reconnect resumes only from the committed local cursor.
- Archive pages pass through the message version/tombstone reducer and cannot
  resurrect a revoked message or overwrite a newer edit. Access revocation
  removes the visible session projection while preserving history; an explicit
  history reset deletes both the session projection and its messages. Terminal
  unread snapshots and later read/pin/mute projections also honor the session
  tombstone, so they cannot recreate a removed placeholder row.
- Membership events carry the recipient identity and member snapshot needed to
  create a complete private-session peer projection locally. A newly created
  private conversation therefore does not require a second session-detail
  request merely to discover its peer.
- Message send, revoke, read, history reset, session pin/mute, and peer pin/mute
  use deterministic or generated command IDs and a durable outbox in v2.
  Optimistic message/session/peer state and its command are committed
  atomically where the UI updates optimistically. Outbox flushing is serialized
  so simultaneous triggers cannot create parallel client dispatch loops;
  retries reuse the same command ID. Terminal business/4xx failures atomically
  remove the poison command and restore its pre-command local projection;
  network, 408, 429, and 5xx failures retain the command for backoff retry.
- If a deployment disables the v2 gate after commands were queued, the v1
  fallback drains previously persisted REST mutations while leaving legacy
  send/read/history retries with their existing single owners. Confirmed
  ordinary commands are removed from the local outbox; only one compact
  history-reset receipt is retained per locally deleted session to prevent
  reconnect resends.
- Pending `sending` rows from pre-v19 clients are backfilled into the outbox.
  Send acknowledgements reconcile the optimistic message and receipt in one
  transaction.
- Web implements the accepted reduced scope: an exclusive per-account writer
  lease (5-second renewal, 15-second expiry). A second tab remains read-only
  and does not open another writer socket. `BroadcastChannel` forwarding and
  automatic leader handoff remain a separately reviewed future enhancement.

### 1. One logical durable stream per user

The server owns one monotonically ordered durable event stream per user. Every
client installation has its own committed cursor for that user. Cursors are not
shared between devices.

The stream contains durable chat-domain changes:

- `message.upsert`
- `message.revoke`
- `session.upsert`
- `session.remove`
- `session.unread_set`
- `session.read_state`
- `session.pin_changed`
- `session.mute_changed`
- `membership.changed`

`inbox_seq` is a sparse monotonic token, not a promise that every integer
exists. A missing integer is not evidence of message loss. The server response
must supply the scan watermark that the client may commit.

### 2. One client synchronization owner

`SyncEngine` is the only component allowed to turn remote durable data into
local durable state. It owns:

- the authenticated WebSocket synchronization lifecycle;
- reconnect/resume and one in-flight batch;
- server-event decoding and ordering;
- cursor persistence;
- history/bootstrap demand;
- the durable outbound command queue;
- retry, backoff, and stale-response rejection.

UI controllers and repositories never call chat/session REST APIs directly.
They may request local data or submit a demand/command to `SyncEngine`, but
network transport remains hidden behind the synchronization boundary.

One physical WebSocket may multiplex three disjoint logical lanes:

1. ordered downstream durable events;
2. idempotent upstream commands;
3. demand-driven archive/bootstrap batches.

These lanes share transport and lifecycle, but not cursor semantics. Archive
history never advances the durable event cursor.

### 3. Database-first publication

The required ordering is:

```text
remote batch
  -> one SQLite transaction
  -> entity changes + projection changes + cursor commit
  -> COMMIT
  -> typed local change notification
  -> UI repository/view model update
  -> sync acknowledgement
```

No durable network handler may mutate a page-owned reactive list before the
database transaction commits. A local change may carry committed rows to avoid
an immediate reread, but it must be emitted by the local-store boundary after
commit, never directly by transport code.

### 4. Durable cursor state

Add a local `sync_state` table keyed by account and stream:

```text
user_id
stream_name
committed_cursor
server_head_cursor
generation
bootstrap_cursor
updated_at
```

The message table's maximum `inbox_seq` and SharedPreferences are not cursor
authorities. Message rows can be collapsed, edited, or revoked, so their
maximum cannot prove that every earlier event was applied.

The cursor is updated in the same SQLite transaction as the entities and
projections produced by its batch. An ACK is sent only after commit. A crash:

- before commit causes the batch to be replayed;
- after commit and before ACK also causes replay, which becomes a no-op;
- after ACK resumes from the committed cursor.

### 5. Sparse-cursor protocol contract

The implemented synchronization envelope is (all cursor/version JSON values
are encoded as decimal strings):

```text
sync_resume {
  "generation": string,
  "committed_cursor": int64-string,
  "capabilities"?: string[]
}

sync_batch {
  "generation": string,
  "from_cursor": int64-string,
  "next_cursor": int64-string,
  "head_cursor": int64-string,
  "has_more": boolean,
  "events": [{
    "cursor": int64-string,
    "kind": string,
    "entity_type": string,
    "entity_id": string,
    "entity_version": int64-string,
    "tombstone"?: boolean,
    "command_id"?: string,
    "payload": object
  }],
  "final_state_snapshot"?: { "unread_by_session"?: object }
}

sync_ack {
  "generation": string,
  "committed_cursor": int64-string
}
```

Auth request and acknowledgement fields are:

```text
auth.capabilities?: string[]
auth_ack.capabilities?: string[]
auth_ack.active_sync?: "v1" | "v2"
```

Resume rejects a cursor beyond the committed server head. The diagnostic
device row never forces a client cursor forward: the client's locally
committed database cursor remains authoritative on every resume. ACK must
match the active generation and the exact pending `next_cursor`.

There is at most one unacknowledged batch per connection. New server activity
marks the connection dirty and wakes the same drain loop. It does not send the
same durable payload through an independent push writer.

`next_cursor` is the cursor of the last event returned in the per-user batch.
Unlike legacy `inbox_seq`, v2 cursor allocation and event insertion share the
same transaction, so committed positions for one user are contiguous. An
empty batch therefore keeps `next_cursor == from_cursor`; neither server nor
client may infer or skip a cursor from a row count or head value.

#### Safe publication watermark

`sync_v2` must not reuse the current Redis `INCR` value as a published stream
head. Redis allocation happens before the surrounding database transaction
commits, so transaction A may reserve sequence 10, transaction B reserve and
commit sequence 11, and a reader may observe 11 before 10. Publishing 11 as a
completed watermark would allow a client to skip 10 forever.

The v1 compatibility path is corrected immediately by acquiring sorted
per-user PostgreSQL advisory transaction locks before **both** Redis and DB
allocation. The lock is released only by commit/rollback, so a second
transaction cannot allocate or commit the next sequence for that user before
the first finishes. This makes current committed `user_inbox` visibility
monotonic without treating rollback-created gaps as events.

Before enabling `sync_v2`, add a PostgreSQL `user_sync_heads` row per user and
serialize stream publication inside the business transaction:

1. normalize and sort all affected user IDs;
2. lock their `user_sync_heads` rows in sorted order;
3. increment each head inside the transaction;
4. insert the corresponding durable events using those values;
5. commit entity state, events, and heads atomically.

The lock prevents another transaction for the same user from publishing a
later head until the earlier transaction has committed. A rollback publishes
neither the event nor its head. Consequently the committed head is a safe
watermark. Redis may remain a notification/cache accelerator, but it is not
the v2 cursor authority.

The server reads v2 events and the committed head from the primary database.
`store.Read()` is forbidden for this path until replica reads carry an explicit
primary commit watermark and tests demonstrate that rows up to that watermark
are visible.

Until every supported client understands this protocol, `auth_ack` advertises
`sync_v2`. Exactly one protocol version is active on a connection; new and old
paths must never run simultaneously.

### 6. Idempotent entity reducer

Every durable entity stores the last applied server version/event cursor.
Incoming state is applied only when newer. Within a batch, repeated operations
for one entity are folded to the final state before SQLite writes.

For stream events, the per-user event cursor is the entity version stored in
the local row as `last_event_seq`. For archive/bootstrap projections, the
server supplies `state_version`; a projection without a comparable version may
insert a missing entity but may not overwrite an existing versioned row.
Server message state also gains an update/version value for snapshot and
history responses; relying only on `msg_id` or `created_at` is not sufficient.

Revoked/deleted state retains a lightweight tombstone or version so archive
backfill cannot resurrect it. Database apply returns explicit change sets:

```text
inserted IDs
updated IDs
deleted IDs
unchanged IDs/count
```

Only inserted, updated, or deleted entities produce local change events.

### 7. Local read models

Pages consume repositories backed by SQLite:

| Consumer | Local source |
| --- | --- |
| Chat window | ordered `messages` query/change stream |
| Conversation list | paged `sessions` projection |
| Conversation preview | session projection fields |
| Search | local session/message search |
| Bottom-tab badge | `account_counters` projection |
| OS badge | the same `account_counters` change stream |
| Send state | message row plus durable outbox status |

The conversation controller must not fetch and render a server summary page.
The chat controller must not fetch recent history merely because a session is
entered or a socket reconnects.

### 8. Absolute unread projections

Unread synchronization uses absolute per-session values and a server version,
not client-side `+1` as the convergence mechanism. Applying a duplicate
`session.unread_set` is a no-op.

Add a local `account_counters` projection containing at least:

```text
total_unread
notification_unread
muted_unread
mention_unread
```

When a session's unread value changes, the same transaction updates the
session row and aggregate counters by the old/new difference. The bottom bar
and OS badge therefore observe one local row rather than rescanning all
sessions or querying the server.

An authoritative compact snapshot may be attached only to the terminal catch-
up batch for self-healing. It is not recomputed for every intermediate batch.

### 9. Durable outbound outbox

User mutations are local-first commands. Add an `outbox` table containing an
idempotency key, command kind, payload, attempt state, and retry schedule.

For example, sending a message atomically inserts the optimistic message and
its outbox command. `SyncEngine` transmits the command, and the server's
canonical result returns through the durable event stream. The reducer then
reconciles the optimistic row with the server message.

This model also applies to edits, revokes, reads, pins, mutes, and session
deletion. In-memory override maps must not be the only protection against a
stale server snapshot because they disappear on process death.

### 10. Bootstrap and old history

A fresh client must not replay the user's entire lifetime stream from zero.
Bootstrap through the synchronization owner establishes a server watermark and
stores all conversation summaries/unread projections needed for the home page.

Message content follows a bounded cache policy:

- recent messages for the active/recent conversation window may be included;
- opening a locally empty conversation creates a history demand for
  `SyncEngine`;
- upward scrolling reads local rows first and creates an older-history demand
  only after the local window is exhausted.

The page remains subscribed to local data while demand is fulfilled. Archive
results are written through the same local reducer, carry entity versions, and
never advance the durable event cursor.

### 11. Durable versus ephemeral events

Typing, presence, socket state, and AI token chunks remain ephemeral in-memory
events. They may use the same physical WebSocket but are not inserted into the
durable event stream and do not write SQLite per token. The finalized AI
message is committed once as durable state.

Media bytes remain on the media/CDN path. The durable stream stores message
metadata and stable media references, not large binary payloads.

### 12. Web multi-tab ownership

The current in-process local change bus cannot notify another browser tab.
Web uses one elected synchronization leader per account/database:

- the leader owns the WebSocket and writes SQLite;
- commits are announced through `BroadcastChannel` using commit IDs;
- follower tabs query the shared local database on notification;
- if the leader exits, another tab acquires the lock and resumes from the
  committed database cursor.

If browser storage cannot be shared in a platform mode, each isolated store is
treated as an independent client instance with its own cursor. Correctness is
preserved at the cost of another connection.

This full leader/follower design is deferred beyond the initial rollout. The
first Web implementation acquires an exclusive per-account writer lock. A tab
that cannot acquire it remains read-only and shows a takeover/reload affordance;
it must not open a second writer connection. `BroadcastChannel` command
forwarding and automatic leader handoff require a separate reviewed design with
notification-loss recovery before they are enabled.

### 13. Existing command migration map

| Existing durable command/path | Target event | Rollout slice |
| --- | --- | --- |
| `push_msg`, message rows in `pull_sync_resp` | `message.upsert` | M3a |
| `push_edit` | `message.upsert` with newer version | M3a |
| `push_revoke` | `message.revoke` tombstone | M3a |
| `session_read_sync`, `unread_sync` | `session.read_state`, `session.unread_set` | M3b |
| `session_member_changed` | `membership.changed`, optional `session.upsert` | M3b |
| `session_access_revoked` | `session.remove` / access tombstone | M3b |
| Durable session-summary/snapshot mutations | `session.upsert` | M3c |
| REST pin/mute mutations and reconciliation | `session.pin_changed`, `session.mute_changed` | M3c |

M3a, M3b, and M3c are separate feature-gated rollouts. A command moves only
after all of its producers, visibility rules, old-client consumers, and local
reducer tests are covered. `session_activity_sync` carries composing/viewing
presence and remains on the ephemeral lane; it is not a durable producer and
is not inserted into `user_sync_events`.

## Current implementation mapping

Useful foundations to retain:

- `LocalDb` and platform-specific database factories;
- `messages` and `sessions` local tables and local search;
- local-first initial chat-window reads;
- `LocalDbChangeBus`, after moving publication responsibility into the store;
- the server `user_inbox` ordering index and event-kind concept;
- idempotent send keys/client message IDs.

Paths that must be removed or narrowed:

- automatic latest-page history reconcile on session entry;
- active-session history refresh after reconnect;
- the force-reload double history request;
- conversation controller rendering REST summaries directly;
- cursor recovery from `MAX(messages.inbox_seq)`;
- concurrent or uncorrelated `pull_sync` calls;
- direct UI/Rx mutations from durable WebSocket handlers;
- unconditional local change events for no-op database writes;
- deletion of old inbox records when appending revoke events.

## Migration plan

### Milestone 1: enforce the local-read boundary

- Make conversation and chat controllers read local repositories only.
- Persist all existing server snapshots before publication.
- Remove automatic recent-history reconciliation from enter/reconnect paths.
- Add single-flight/coalescing to existing pull and archive requests.
- Emit local events only for actual database changes.

The message/list hot path of this milestone is implemented by this change. It
is wire-compatible and provides the immediate energy and duplicate-work
reduction. The remaining legacy summary code is disabled by default and
retained only for rollback; archive history remains demand-driven for empty
bootstrap and explicit upward pagination. Legacy durable command handlers are
removed only after the Milestone 2 reducer and the matching Milestone 3 event
slice are enabled.

### Milestone 2: transactional client state

- Add `sync_state`, `outbox`, entity versions/tombstones, and
  `account_counters`.
- Introduce one transactional batch reducer.
- Route all durable downstream handlers through the reducer.
- Persist optimistic commands and retry state.
- Replace volatile unread/pin override ownership with durable pending commands.

Implemented in local schema v19 and the `LocalDb` synchronization repository.
The reducer, cursor, projections, command receipts, and account counters share
one transaction; optimistic mutations use the same database plus outbox.

### Milestone 3: server/client `sync_v2`

- Make the per-user server event log append-only.
- Cover message, session, membership, and unread state producers.
- Add resume/batch/ack protocol and terminal snapshots.
- Add capability negotiation and mutually exclusive v1/v2 connection modes.
- Query the synchronization stream from the primary database until a replica
  watermark contract exists.

Both halves are implemented, including producer coverage, negotiation,
resume/batch/ACK, replay, contiguous safe watermarks, independent device state, terminal
unread snapshots, first-use bootstrap, local reduction, and commit-before-ACK.
The feature gate remains off by default until deployment and telemetry gates
are deliberately enabled.

### Milestone 4: cleanup and Web leadership

- Remove legacy independent durable push writers after the compatibility
  window.
- Remove page-owned conversation/history network paths.
- Add Web leader election and cross-tab commit notifications.
- Remove obsolete SharedPreferences cursor ownership and volatile overrides.

Legacy durable payloads are suppressed on negotiated v2 connections while v1
writers remain for rolling upgrade. Page hot paths read local projections, and
Web enforces one writer with the lease described above. Full cross-tab command
forwarding, commit notifications, and automatic handoff are outside this
reviewed scope and remain deferred rather than being prerequisites for rollout.

## Rollout and compatibility

1. Apply migration 127, then deploy append-only server behavior and additive
   protocol fields to every backend node while the v2 gate remains disabled.
2. Deploy clients capable of `sync_v2`, defaulting to v1 when capability is
   absent.
3. Enable `sync_v2` by feature gate for internal accounts, then a percentage
   rollout.
4. Observe duplicate rate, cursor lag, reconnect recovery, batch duration,
   SQLite changed/unchanged counts, and crash-free sessions.
5. Disable the gate immediately on regression; local schema additions remain
   backward-compatible.
6. Remove v1 only after the minimum supported client version no longer needs
   it.

The database migration must be additive. Old clients ignore new server fields,
and new clients must never enable v2 unless the server advertises support.

For backend rollback, set `AIBOT_SYNC_V2_ENABLED=0` and recycle WebSocket
connections so every reconnect negotiates v1. Do not drop the four new tables
or version columns: leaving them in place is the rolling-upgrade-safe rollback.
The legacy `user_inbox`, `pull_sync`, and durable v1 push paths continue to be
written throughout the compatibility window. Re-enabling the gate therefore
requires no destructive migration or reconstruction of v1 state.

## Correctness invariants

1. A committed cursor never covers a durable event that failed local apply.
2. A page never displays uncommitted remote durable state.
3. Applying the same batch twice produces no second entity write or UI event.
4. A stale entity version never overwrites newer local canonical state.
5. Archive history never advances the durable stream cursor.
6. A tombstone prevents history from resurrecting deleted state.
7. One connection has at most one unacknowledged durable batch.
8. One local database has at most one active synchronization writer.
9. Every server durable mutation and its user-stream event commit atomically.
10. Each device resumes from its own database cursor; no device consumes on
    behalf of another device.

## Failure and recovery matrix

| Failure point | Expected recovery |
| --- | --- |
| Disconnect before local commit | Resume from old cursor; replay batch |
| Crash after commit before ACK | Resume from committed cursor or receive a no-op replay |
| Duplicate/reordered response | Reject generation mismatch or no-op by entity version |
| SQLite write failure | Do not advance cursor or ACK; back off and retry |
| Outbox send timeout | Retry same command ID; server idempotency prevents duplicate action |
| History response races a newer event | Entity version/tombstone keeps newer state |
| Web writer tab closes | Lease expires; a reloaded/reconnected tab can acquire it and resume the database cursor |
| Server read replica lags | Sync stream remains on primary until watermark-safe reads exist |

## Verification

Required automated coverage:

- contiguous per-user cursor enforcement and zero-progress empty batches;
- more than one server batch;
- simultaneous resume/manual/realtime triggers produce one in-flight drain;
- crash before commit, after commit, and before ACK;
- replay produces zero changed rows and zero UI events;
- message edit/revoke followed by old history backfill;
- fresh install bootstrap racing with a new message;
- independent cursors for two devices;
- reconnect does not request active-session recent history;
- entering a non-empty chat issues no network request;
- conversation list and bottom badge render with networking disabled;
- outbox restart/retry and server idempotency;
- exclusive Web writer lease, renewal, rejection of a second writer, release,
  and expiry takeover. Automatic handoff and follower commit notification are
  deferred with the full multi-tab design.

Backend Milestone 2/3/4 verification executed on 2026-09-22:

- focused tests passed for `syncstream`, store migration discovery, producer
  services, agent history/finalization, protocol, Redis dispatch, WebSocket
  negotiation/drain, replay, disconnect, cursor continuity, batches over 100,
  two-device state, generation replacement, command retry, visibility, and v1
  compatibility;
- `go test -race ./internal/ws/handler -run 'TestSyncV2|TestHandleSessionRead'
  -count=1` passed, as did the focused service race tests for edit/session
  producer paths;
- `AIBOT_TEST_NATS_URL=nats://127.0.0.1:1 go test ./...`, `go vet ./...`, and
  `go build ./...` passed;
- real PostgreSQL 18 `pgverify` passed for both the v2 transactional head and
  the legacy inbox advisory-lock path. Each test holds transaction 1 open,
  proves transaction 2 remains blocked, then verifies commit/rollback exposes
  exactly the expected next cursor;
- the complete SQL migration chain was executed against a separate disposable
  PostgreSQL database with pgvector installed. Migration
  `127_local_first_sync_v2.sql` applied successfully and all four v2 tables
  were verified. The temporary clusters were stopped and moved to Trash after
  validation.

Client verification executed on 2026-09-22 includes schema-v19 migration,
transactional reducer/rollback/replay, stale archive and tombstone rejection,
versioned session bootstrap, access-revoke versus history-reset semantics,
optimistic projection plus outbox atomicity, pre-v19 pending-message backfill,
writer-lease exclusion/expiry, v1/v2 negotiation, commit-before-ACK, cursor
mismatch recovery, durable mutation receipts, and single-path history-reset
transport. Focused Flutter tests pass, and Flutter analysis has only the
pre-existing `prefer_const_constructors` information finding in
`chat_ai_identity_test.dart`. The final full Flutter run passed 2,974 tests
with four intentional skips and zero failures. All 25 changed Dart files also
pass `dart format --output=none --set-exit-if-changed`.

Operational acceptance targets:

- warm chat/list navigation performs zero chat-data network requests;
- idle foreground/background performs no sync work beyond required heartbeat;
- each durable batch uses one SQLite transaction;
- one durable server event payload is transferred once in `sync_v2`;
- duplicate batch database writes and UI publications are zero;
- unread/session/message state converges after reconnect without page refresh;
- Instruments shows materially fewer network wakes and SQLite writes than the
  current build under the same scripted group-chat workload.

Numeric rollout gates for the scripted 10-minute group-chat workload and
production telemetry are:

- exactly zero recent-history or conversation-summary requests during warm
  chat/list navigation;
- no more than one unacknowledged/in-flight durable batch per client database;
- zero changed SQLite rows and zero UI publications for an exact replay;
- one SQLite transaction per durable batch of up to 100 stream entries;
- foreground cursor lag p95 below 2 seconds and p99 below 10 seconds when the
  socket is healthy;
- batch apply p95 below 100 ms for 100 entries on the supported baseline iOS
  device;
- at least 50% fewer chat-related SQLite writes and network wakeups than the
  pre-change build in the same Instruments trace;
- no increase above 0.1% in sync retry loops, reconnect loops, crash-free
  session regression, or unread divergence reports during each rollout stage.

Crossing a correctness invariant, observing any permanent cursor skip, or
exceeding a regression threshold disables the relevant feature gate; rollout
does not proceed while relying on history-page reconciliation as a hidden
repair path.

## Alternatives

### Keep realtime push plus pull plus page reconciliation

Rejected. Idempotent database writes limit corruption but do not remove network,
decode, query, transaction, and UI scheduling costs. Multiple repair paths also
make cursor correctness difficult to prove.

### Make every page query the server and use SQLite only as a cache

Rejected. This couples UI lifecycle to network lifecycle, duplicates requests,
weakens offline behavior, and makes different views observe different states.

### Replay the entire user stream on every fresh install

Rejected. It has simple semantics but unbounded startup cost. A bounded
bootstrap snapshot plus a watermark gives the same incremental correctness.

### Persist AI stream chunks as durable events

Rejected. Token frequency would create avoidable database and rendering load.
Only finalized message state is durable.

## Consequences

- Client screens become deterministic local projections and remain useful
  offline.
- Reconnect, resume, and page navigation no longer compete to repair data.
- The synchronization engine and reducer become critical infrastructure and
  require strict protocol/version tests.
- Fresh-install bootstrap and browser multi-tab ownership need explicit
  lifecycle handling.
- Server storage grows because the event log becomes append-only; retention
  requires a separate compaction/checkpoint design that never invalidates a
  supported client cursor.
- During rollout, v1 and v2 code coexist, increasing temporary maintenance
  cost, but a connection activates only one mode.
