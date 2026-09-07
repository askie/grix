# Direct-chat messages carry session members

## Context

Clients group the conversation list by peer. A direct chat row is keyed by
`private:<peer_type>:<peer_id>`; every other row is keyed by `session:<id>`.
The server builds those keys from `session_members`, but chat message payloads
carried no peer identity, so a client could only infer one from the sender.

Messages whose sender is not the peer break that inference: system messages
(`sender_type=3`) such as reach notices, and the first message of a new thread
that the peer did not send. They created local session rows with an empty
`peer_id`, whose group key degraded to `session:<id>` and no longer matched the
server summary row. Unread reached the bottom tab badge (a flat sum over local
sessions, no grouping) while the conversation list showed nothing, until a
peer-identity backfill round trip or the next summary refresh corrected it —
seconds to a minute later.

## Decision

`PushMsgPayload` and `pull_sync_resp` message items carry
`session_members: [{member_id, member_type}]` for direct chats. The set is
resolved once per message, not per recipient; the client picks the member that
is not itself, matching the summary rule "exclude only the human member whose
id is me".

Two categories are deliberately excluded:

- Group chats: their group key is already the session id, so members would only
  enlarge the packet.
- Website-visitor (widget) sessions: clients render all visitor sessions as one
  synthetic row and never group them by peer, and the receiving end is an
  anonymous web visitor who would otherwise learn the site owner's user id from
  a message the owner never sent.

## Alternatives

- Per-recipient peer field. Rejected: the delivery fan-out would have to compute
  a different payload per member for no gain, since the client can pick the
  non-self member itself.
- Client-side backfill only (fetch session detail whenever a peerless session
  appears). Rejected as the primary fix: it costs a round trip per session, is
  rate-limited and retry-capped, and leaves the visible gap the bug is about.
  It is kept as the fallback for older servers.
- Filling the field inside the broadcast helpers. Rejected: it would hide a
  database read inside a per-recipient fan-out loop.

## Consequences

- Additive and optional: older clients ignore the field, and a newer client
  talking to an older server falls back to sender-derived identity plus the
  existing backfill.
- One indexed `session_members` read per direct-chat message on the send path;
  `pull_sync` resolves a whole page in a single batched query to avoid N+1.
- Widget visitors keep seeing exactly what they saw before.

## Verification

- `backend/internal/api/service/session_member_identity_test.go`: direct, group,
  widget, and batch resolution.
- `backend/internal/ws/handler/send_msg_test.go`,
  `backend/internal/ws/handler/pull_sync_test.go`: the field is filled for
  direct chats and absent (including from the serialized JSON) for group chats.
- `frontend/test/modules/home/controllers/conversations_unread_peer_identity_test.dart`:
  a `sender_type=3` message on a new thread aligns the list row with the tab
  badge in the same event-loop turn with no network call, and publishes the list
  at most once.

## Follow-up (2026-09-07): the display hole, not the identity source

Carrying `session_members` removed one *producer* of peerless direct-chat rows.
It did not close the *display* hole those rows fall into, and the symptom kept
coming back.

In the conversation-summary path the list is built from server summary rows plus
a local backfill for groups the snapshot does not carry. That backfill refused
any local direct-chat session with an empty `peer_id`, on the assumption that
peer-identity backfill would soon merge it into the peer group. When the round
trip never resolves — `4003`/`4004` sessions, a member set that resolves to
nothing, retry budget spent — the session's unread stays in the bottom badge
(a flat sum over local sessions) and has no row at all. That residue is the
recurring "badge has unread, the list shows none".

Decision: the badge total is the invariant. Peerless direct-chat sessions are
now rendered as their own `session:<id>` row, but only up to the shortfall
between the bottom badge and the sum of the displayed rows, most recent first.
Server summaries group by peer and usually already count that unread inside the
peer row, so an unconditional backfill would show the same unread twice; gating
on the shortfall covers the gap and nothing more. It stays a pure in-memory pass
inside the existing summary replay — no timer, no request, no change to any
refresh interval, and still one list publish per message.

The widget-visitor exclusion above stands, but it needs a client-side
counterpart. `sessions` has no `is_visitor` column, so persisting conversation
summaries into LocalDb (for local search) wrote widget conversations back as
plain `type=private, peer_id=''` rows: after the next `loadSessions` they
returned as exactly the peerless direct-chat rows this note is about, were
chased by pointless session-detail backfill, and a visitor message carrying a
sender id even split them out of the synthetic visitor row into a bogus
`private:1:<visitor id>` row. The summary already carries `is_visitor`, so the
persist path now records visitor identity in memory alongside the write.
Group chats are unaffected: their group key is the session id, so they were
never in the refused class.

Verification:
`frontend/test/modules/home/controllers/conversations_unread_peer_identity_test.dart`
— unresolvable peer identity still puts the unread on the list and matches the
badge; the shortfall gate does not double-count a summary row that already
counts it; a persisted visitor conversation survives a reload as a visitor
session and stays in the single visitor row.
