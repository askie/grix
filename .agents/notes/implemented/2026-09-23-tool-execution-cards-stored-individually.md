# Tool execution cards are stored individually and grouped by clients

## Context

The agent API used to merge a turn's tool calls into one `tool_execution_group`
message and rewrite it in place on every call. Each rewrite updated the whole
row, appended a full `message.upsert` per member to the sync stream, and pushed
the whole card again, so data grew quadratically with the number of tool calls.
Because the merge key was `(agent, session)` and only reset on a new turn or an
approval card, tool calls after an agent text reply were folded back into the
card above that text. The Flutter client already folds adjacent single
`tool_execution` messages from the same sender, but never received them.

## Decision

- The server persists every tool call as its own `tool_execution` message and
  never edits earlier tool messages. Exact retries (same event and tool call id,
  or same client message id) still resolve to the stored message.
- Clients group adjacent tool cards for display; any other message in between
  starts a new group.
- Tool execution cards (single or grouped) do not raise the server unread count
  (DB and Redis). They are still delivered and still advance session activity.
  No client change: sync V2 clients take unread from `session.unread_set`, and
  legacy push clients reconcile from the unread snapshot on the next pull.

## Alternatives

- Keep a server-side group but persist it once at the end, streaming only
  per-call deltas to clients: fewer rows, but needs a new realtime event, a
  close/timeout path, and still hides the group from reconnecting devices.
- Keep the in-place group and reset it on agent text: fixes grouping but keeps
  the quadratic rewrite cost.

## Consequences

- More message rows per turn (a few hundred bytes each); storage and sync
  traffic per turn become linear.
- Existing `tool_execution_group` rows remain valid and render unchanged.
- Clients render the new single cards correctly (they already group them).
  Only legacy (non sync V2) connections still count them as unread locally
  until the next unread snapshot.
- A group split across history pages shows as two groups until the older page
  is loaded.

## Verification

- `backend/internal/ws/agentapi/tool_exec_cards_test.go`:
  `TestHandleSendMsg_StoresEachToolExecutionCardSeparately`, reservation tests.
- `backend/internal/ws/handler/send_msg_test.go`:
  `TestHandleSendMsgToolExecutionCardDoesNotRaiseUnread`.
- `frontend/test/modules/chat/message_cards/chat_tool_execution_group_card_projection_test.dart`:
  `splits groups when text message breaks the sequence`.
