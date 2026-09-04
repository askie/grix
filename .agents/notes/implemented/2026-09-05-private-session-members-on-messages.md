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
