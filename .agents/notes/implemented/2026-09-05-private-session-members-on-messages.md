# Direct-chat messages carry session members

## Context

Conversation grouping uses `private:<peer_type>:<peer_id>` for direct chats
and `session:<id>` otherwise. Inferring peer from sender fails for system
notices and new threads the peer did not send: unread reaches the badge but a
peerless local row cannot match its summary group.

## Decision

Legacy `PushMsgPayload` / `pull_sync_resp` message items optionally carry
`session_members:[{member_id,member_type}]` for direct chats. Resolve the set
once per message (batch once per pull page), not once per recipient. The
client excludes only the human member whose ID is itself to select the peer,
matching summary grouping. Sender-derived identity and bounded detail backfill
remain fallback for older servers.

Exclude group chats (already keyed by session) and **widget visitor sessions**:
visitors share one synthetic row and must not learn the site owner's user ID
from a message the owner never sent. A per-recipient peer payload would add
fan-out work with no benefit; a hidden DB read inside broadcast is also rejected.

The summary rollback path retains a separate display safeguard: render
peerless private sessions as `session:<id>` rows, newest first, only until the
shortfall between the badge total and displayed rows is covered. Identity
backfill can fail permanently (4003/4004, empty members, exhausted retries);
waiting for it would hide unread forever. Unconditional stubs would double
count unread already in a peer summary. This is an in-memory pass, with no
new timer/request and one list publication.

When persisting summaries for local search, retain `is_visitor` in the client's
visitor-identity set; the local session table has no such column. Reload and
sender inference must keep visitors in the synthetic visitor row and skip
pointless peer-detail backfill.

The default local read model and v2 membership/session snapshots now provide
peer identity through the
[local-first pipeline](2026-09-21-local-first-single-sync-pipeline.md).
The older summary path is enabled only by `USE_CONVERSATION_LIST_API=true`;
its shortfall/visitor safeguards are still present, not a default network
repair requirement.

## Alternatives

- Per-recipient peer computation: unnecessary fan-out complexity.
- Detail backfill as the primary identity source: extra latency, rate/retry
  limits and a visible unread gap; keep it only for compatibility.
- Drop unresolved rows or add every stub: respectively hides or doubles unread.

## Consequences

Optional fields preserve old/new client compatibility. Membership reads remain
outside recipient loops. Privacy exclusions and visitor grouping apply to all
identity paths, including fallback summary persistence.

## Verification

Paths from repository root:

- `backend/internal/api/service/session_member_identity_test.go`: direct/group/
  widget exclusion and batch resolution.
- `backend/internal/ws/handler/{send_msg_test.go,pull_sync_test.go}`:
  serialized direct members and group omission.
- `frontend/lib/modules/home/controllers/conversations_controller.dart`:
  summary switch and shortfall-limited stubs;
  `frontend/lib/data/providers/im_service_sessions.dart`: visitor identity.
- `frontend/test/modules/home/controllers/conversations_unread_peer_identity_test.dart`:
  system-sender identity, unresolved unread, no double count, visitor reload,
  same-turn list/badge agreement with no network and at most one publication.
