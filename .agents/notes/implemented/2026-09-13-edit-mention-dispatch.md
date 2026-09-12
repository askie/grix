# Message edits only dispatch to newly added @mentions, once, ever

## Context

`EditMessage` (`internal/api/service/message_edit_service.go`) only ever
sent a lightweight `edit` sync event (`CmdPushEdit`/`CmdEventEdit`) so open
clients refresh the message in place. It never entered the agent event
routing that a brand-new message goes through
(`internal/ws/handler/direct_session_route.go`), so adding an `@AgentName`
mention while editing a message — e.g. a task-flow lead agent editing its
own flowchart status message to hand a step to another agent — was silently
a no-op: the mentioned agent never woke up.

The obvious fix — treat every edit like a new message and re-run mention
dispatch on its full current mention set — was rejected before
implementation started. A flowchart status message edited many times over a
task's lifetime would re-wake every already-mentioned agent (and re-notify
every already-mentioned human) on every save, turning routine progress
updates into repeated group-wide interruptions.

## Decision

- Only the **newly added** mentions in an edit can trigger anything:
  `newMentions - oldMentions` (both resolved the same way the send path
  resolves them — `mention.ParseUserIDsWithCandidates` against
  `ResolveMentionCandidatesForSession`), computed against the message's
  state immediately before this specific edit. Removing a mention, leaving
  a mention alone, or any edit that touches neither the mentioned set nor
  the underlying member set is inert by construction — no event is even
  considered.
- Each `(msg_id, member_id)` pair can be dispatched at most once, ever, not
  just once per edit. This is enforced by a durable claim in a new table,
  `message_mention_dispatch_receipts` (migration 126), inserted with
  `ON CONFLICT DO NOTHING` before dispatch — a member added, then removed,
  then re-added across separate edits is only notified the first time. An
  in-memory or per-edit-only diff cannot catch this: it only sees the two
  states immediately either side of one edit, not the message's whole
  history.
- Newly mentioned **agents** get an explicit delivery
  (`ws/handler.DispatchMessageEditMentionAdditions`, called from all three
  `EditMessage` call sites — the two HTTP edit endpoints and the Agent API
  WS bridge's `message_edit` invoke action — after the edit's own
  transaction has committed). It reuses `resolveDirectSessionRoute` /
  `dispatchDirectSessionRoute` exactly as a new message does, restricted to
  just the newly added target IDs via a synthetic `groupDispatchSemantics`
  with `TargetUserIDs`/`ExplicitMentionUserIDs` set to only those IDs. This
  is what a new message would do for that agent — same proprietary-agent
  mention-only gating, same `agentreceive.Evaluate` receive-mode policy,
  same self-sender skip, same approval-issuer exemption — none of it
  reimplemented. The resolved route's `MirrorTargets` are discarded before
  dispatch: mirror fan-out is for new messages reaching every non-targeted
  API agent's context log, and re-running it on every mention-adding edit
  would re-notify agents this edit has nothing to do with.
- Newly mentioned **humans** get no new code path. They are already session
  members who received the message at send time; the edit-sync payload
  already carries the updated `extra.mention_user_ids` for client-side
  highlighting. There is no separate "you were @mentioned" notification or
  unread signal in this codebase distinct from the ordinary per-message
  unread bump a group member already got when the message was first sent.
- The event carries `edited: true` and `edited_msg_id` (`DelegateEventPayload`
  in `internal/ws/agentapi/manager.go`) so a connector can tell this apart
  from the message's original send. `edited_msg_id` always equals `msg_id`
  (the message is rewritten in place, not recreated) but is a separate field
  so a connector need not rely on that always being true.
- Private (1:1) sessions are untouched: the send path already strips
  `mention_user_ids` entirely for non-group sessions
  (`resolveGroupMentionNormalization`'s non-group branch), so there is no
  mention set to diff there. `DispatchMessageEditMentionAdditions` is a
  no-op whenever `loadSessionType(sessionID) != 2`.
- The send path also claims the same receipt: `dispatchDirectSessionRoute`'s
  main (non-mirror) `AgentProviderAPI` branch now claims
  `message_mention_dispatch_receipts` for every genuinely, individually
  named `@mention` it actually dispatches (new `directDispatchTarget.
  ExplicitSingleMention`, deliberately distinct from `Mentioned` — a fresh
  `@所有人` also sets `Mentioned` for every member, but must never claim this
  receipt). Found in acceptance review: without this, a message sent already
  mentioning agent X, then edited to remove `@X`, then edited again to
  re-add `@X`, would re-deliver to X — X's *first* delivery happened at send
  time, which the edit-side diff alone has no record of.

## Correction (post-review)

Initial implementation only wrote receipts from the edit-triggered path,
never from the ordinary send path. That let an edit re-deliver a mention the
send path had already delivered (add @X at send → edit removes @X → edit
re-adds @X → re-dispatches X), violating "each (msg_id, member) fires at
most once, ever." Fixed by claiming the receipt at the send-time dispatch
site too, scoped to `ExplicitSingleMention` so `@所有人`, continuation, and
mirror targets — none of which represent a real "was this specific member
individually @mentioned" fact — never claim it.

## Alternatives

- Re-run full mention dispatch on every edit (dispatch on the edited
  message's current mention set, not the diff): rejected — re-wakes every
  already-mentioned agent/human on every save, exactly the "flowchart edited
  ten times, group pinged ten times" problem this exists to avoid.
- Diff against the immediately-prior edit only, no durable receipt table:
  rejected — cannot prevent the add/remove/add oscillation across separate
  edits from re-dispatching every time the mention reappears.
- Give newly mentioned humans an explicit dispatch too (mirroring the agent
  path): rejected as unnecessary scope — there is no existing "@mention"
  notification primitive for humans separate from ordinary per-message
  unread/push, and building one was not asked for and not needed: a human
  group member already has the message.
- Keep mirror fan-out on edit-triggered dispatch (matching new-message
  behavior exactly): rejected — mirror targets are non-mentioned API agents
  that get a record-only copy of *every* new message for context continuity;
  re-running that on an edit would push updated content to agents this
  specific edit never mentioned, which is exactly the noise this feature is
  trying to avoid introducing.

## Consequences

- `service.EditMessage`'s signature changed from `(...) error` to
  `(...) (*EditMentionDispatchContext, error)`. All three callers were
  updated; `outcome` is `nil` on error and on a genuine no-op edit (content
  and extra both unchanged), so a caller that only checks `err` is
  unaffected.
- `dispatchDirectSessionRoute` gained a trailing `edited bool` parameter;
  its two other call sites (`TriggerDirectRouteForMessage`, `retry_msg.go`)
  pass `false`.
- `internal/api/handler` now imports `internal/ws/handler` (aliased
  `wshandler`) so the two HTTP edit endpoints can reach the same dispatch
  pipeline the WS bridge uses; verified this does not create an import
  cycle (`ws/handler` never imports `api/handler`). The HTTP path passes a
  `nil` hub — already safe, since `dispatchDirectSessionRoute`'s only
  hub-dependent calls (`notifyAgentDeliveryError`, `notifyAgentQueuedOffline`,
  both "let the human sender know delivery is delayed/failed" nice-to-haves)
  already nil-guard `hub`.
- Dispatch runs strictly after `EditMessage`'s transaction has committed and
  never returns an error to its caller; a delivery-layer failure (agent
  channel unavailable, route missing) is logged as a warning and cannot
  fail or delay the edit's own HTTP/WS response.

## Verification

`go test -count=1 ./internal/ws/... ./internal/api/service/...
./internal/api/handler/...` passes. New coverage:
`TestDispatchMessageEditMentionAdditions_{NewAgentMentionDispatchesOnce,
RemovedMentionDoesNotTrigger,PlainTextEditDoesNotTrigger,
RepeatedAddRemoveAddDoesNotDoubleDispatch,
NewHumanMentionSkipsAgentDispatch,PrivateSessionIsNoOp,SelfMentionSkipped,
SendTimeMentionSkipsEditRetrigger,SendTimeMentionDoesNotBlockDifferentAgent,
AgentEditorMentionsAnotherAgentDispatches}` (`ws/handler`) — the two
`SendTime*` cases are the send-claims-the-receipt-too correction: send @X →
edit removes @X → edit re-adds @X delivers to X exactly once (at send time),
and send @X → edit also adds @Y delivers to Y regardless;
`TestEditMessage_{ReturnsMentionDispatchContextOnSuccess,
NoOpEditReturnsNilContext,ReturnsNilContextOnFailure}` (`api/service`);
`TestAgentMessageEdit_NewAgentMentionDispatchesThroughFullStack`
(`api/handler`, end-to-end through the real HTTP route, asserting both the
200 response and the claimed dispatch receipt row — including the delivery
falling back to the offline delegate-event queue without affecting the
response, since no WS manager is configured in that test).
