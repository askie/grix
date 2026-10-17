# Message edits dispatch newly added agent mentions at most once

## Context

Editing a message originally updated client display without waking newly
mentioned agents. Re-running full dispatch on every flowchart/status edit
would repeatedly wake the same agents. A per-edit diff alone also misses
add/remove/add oscillation across the lifetime of one message.

## Decision

- In groups only, compute `newMentions - oldMentions` against the immediately
  preceding content/extra using the send path's candidate resolution and
  `mention.ParseUserIDsWithCandidates`. Unchanged/removed mentions and plain
  text edits are inert; private sends strip the mention set and edits do nothing.
- Each `(msg_id, member_id)` can be claimed at most once via
  `message_mention_dispatch_receipts` (migration
  `backend/migration/126_message_mention_dispatch_receipts.sql`), with
  `ON CONFLICT DO NOTHING` before dispatch. This is a durable lifetime claim,
  not a per-edit or in-memory deduplication.
- **Both send and edit claim the same receipt.** Send claims only genuine
  individually named `ExplicitSingleMention` targets in its main API-agent
  branch. `Mentioned` alone is insufficient: first-time `@所有人`, continuation
  and mirror delivery must not consume an individual mention receipt. Thus
  send @X → remove @X → re-add @X does not wake X twice, while adding @Y can.
- After edit commit, all entry points (both HTTP edit routes and Agent API
  `message_edit`) call `DispatchMessageEditMentionAdditions`. It reuses
  `resolveDirectSessionRoute` / `dispatchDirectSessionRoute`, restricting
  target/explicit-mention IDs to newly added agents: same proprietary-agent
  gating, receive-mode policy, self-sender skip and approval-issuer exemption.
  Clear `MirrorTargets`; unrelated agents must not get another context copy.
- Human members already received the original message; the edit sync updates
  `extra.mention_user_ids` for highlighting. Do not add a separate human
  notification/unread path for edits.
- Delegate payload carries `edited:true`, `edited_msg_id` and original `msg_id`.
  The two IDs currently match, but the explicit field lets connectors distinguish
  rewritten messages without assuming an edit creates a new message.

## Alternatives

- Dispatch the whole current mention set: repeated interruptions on each save.
- Immediate-prior diff without durable send/edit receipts: add/remove/add and
  send-time mentions would dispatch twice.
- Mirror fan-out or a new human mention-notification mechanism: unrelated noise
  and scope; existing human message sync is sufficient.

## Consequences

`EditMessage` returns optional `EditMentionDispatchContext`; error and genuine
no-op edits return nil. Dispatch runs after the mutation transaction, logs
routing/delivery failures and cannot turn a committed edit into an edit error.
HTTP callers may have a nil hub, so auxiliary delivery notices must remain
nil-safe. Receipt claiming provides at-most-once dispatch, not a delivery
retry guarantee. Normal send/retry routes pass `edited=false`.

## Verification

Current paths from repository root:

- `backend/internal/ws/handler/{message_edit_mention_dispatch.go,direct_session_route.go}`:
  diff, group/agent filter, durable claim, send-time individual mention claim
  and mirror removal.
- `backend/internal/ws/handler/message_edit_mention_dispatch_test.go`: new,
  removed and unchanged mentions; add/remove/add; send-time X vs newly added Y;
  human/private/self exclusions; agent editor mentioning another agent.
- `backend/internal/api/service/message_edit_service_test.go`: context on success,
  nil context on no-op/error.
- `backend/internal/api/handler/agent_message_edit_mention_dispatch_test.go`:
  `TestAgentMessageEdit_NewAgentMentionDispatchesThroughFullStack` exercises the
  HTTP entry, claimed receipt and offline-queue delivery without failing edit.
