# Mention-only agents no longer become permanent group continuation targets

## Context

`session_members.agent_receive_mode = 3` (`ModeMentionOnly`) restricts an
agent to explicit `@mentions` in a group. A real customer set their
"发布员" (release) agent to mention-only, but it kept answering anyway.
Every misfire traced back to the customer sending an un-@'d follow-up
("继续", "解决一下") right after that agent had last spoken.

`group_dispatch_semantics.go`'s continuation ladder resolves an un-@'d
group message's target through, in order: explicit mentions, `@所有人`
continuation, a cached 1:1 continuation, and — as the bottom fallback —
whichever agent spoke last (`loadLastAgentContinuationTarget`), with no
regard to that agent's own receive mode. A prior code comment on the
direct-route dispatch gate documented this as intentional: a directed
continuation was meant to count as addressing the agent "for every client
type," including mention-only ones. That intent is what produced the
lock-in: a mention-only agent that replies once becomes "the last
speaker," and every subsequent un-@'d message keeps landing back on it —
self-perpetuating, since each reply makes it the last speaker again, with
no way for the human to opt out short of leaving the group.

## Decision

Exclude `ModeMentionOnly` agents from being *selected* as a continuation
target, in `group_dispatch_semantics.go`, at all three points where a
continuation target is resolved — not only at the final direct-route
dispatch gate:

- `@所有人` continuation (`loadGroupMentionAllContinuation`).
- The cached single-target 1:1 continuation
  (`loadGroupContinuationTargetIDs`).
- The last-agent-speaker fallback (`loadLastAgentContinuationTarget`,
  now `selectContinuableAgentTarget`): when the last speaker is
  mention-only, walk back through recent messages (bounded by
  `lastAgentContinuationLookback = 20`) for the nearest prior agent
  speaker that is not mention-only, instead of just declining the last
  speaker outright.

Unaffected by design: explicit `@mentions` (including a first-time
`@所有人`, which still expands to every member's ID and counts as
explicit); the approval-card round trip back to its issuing agent
(`isApprovalIssuer`, in both `direct_session_route.go` and
`send_msg_delegate.go`); and continuation for non-mention-only agents.

## Alternatives

- Gate only at the direct-route dispatch decision
  (`direct_session_route.go`'s `mentioned || continuedMentionAll ||
  (directedContinuation && targeted) || isApprovalIssuer`): rejected.
  `loadLastAgentContinuationTarget` returns exactly one candidate — the
  last speaker. If that speaker is mention-only and gets rejected only at
  the gate, no other agent is even considered, so an un-@'d follow-up in
  a group where the mention-only agent happened to speak last would be
  silently dropped even when a normal-mode agent (e.g. the customer's
  "大脑") was present and should have picked it up. Filtering has to
  happen where the target is *chosen*, not just where it is checked.
- Let the last-agent fallback return no target instead of walking back:
  rejected for the same silent-drop reason above; walking back to the
  nearest continuable agent preserves the existing "someone should
  usually answer an un-@'d follow-up" behavior for the common
  mixed-mode-group case, while a group with *no* continuable agent still
  correctly gets no dispatch (acceptable — the human deliberately
  restricted every agent present to explicit @mentions).
- Unbounded backward scan instead of `lastAgentContinuationLookback =
  20`: rejected, bounds query cost; `messages(session_id, msg_id DESC)`
  is already indexed (`idx_msg_session_time`, migration 001).

## Consequences

- A mention-only agent's own replies no longer keep it "sticky" as the
  implicit target of the conversation; the human must @ it again (or the
  group's `@所有人` continuation is consumed once and does not carry it
  forward with a plain follow-up going forward).
- `send_msg_delegate.go`'s human-delegate path (a human member delegating
  their own reply duty to an agent, keyed by the *human's own*
  `agent_receive_mode`, not the underlying agent's) was checked for the
  same lock-in and does not need a parallel fix: it consumes the same
  `groupDispatchSemantics.TargetUserIDs` this change already filters, and
  it has no `directedContinuation`-style bypass of its own — a
  mention-only *delegate* target could never be reached by continuation
  alone, mirror-only skip aside (`mirrorMode == RecordOnly &&
  receiveMode == ModeMentionOnly`, unaffected by this change).
- Group-wide `@所有人` (first message, not continuation) still reaches
  mention-only agents, unchanged — `ExplicitMentionAll` short-circuits to
  `hasExplicitGroupMentionTargets` before any continuation logic runs.
  Whether a bare `@所有人` should count as "explicit enough" for a
  mention-only agent is a separate product question, left open.

## Verification

`backend/internal/ws/handler/direct_session_route_test.go`:
`TestHandleSendMsgGroupPlainContinuationAfterMentionOnlyAgentFallsBackToNormalModeAgent`
reproduces the customer scenario end to end (mention-only agent speaks
last, human sends an un-@'d follow-up, mention-only agent gets nothing,
the normal-mode agent gets dispatched). `...MentionAllContinuationSkipsMentionOnlyDirectAgents`,
`...GroupProprietaryMentionOnlyAgentIgnoresContinuationAfterAgentReply`,
and `...GroupGenericMentionOnlyAgentDoesNotProcessContinuationAfterOwnReply`
cover the no-fallback-available case (silent no-dispatch is correct).
`...QuotedTargetWakesMentionOnlyDirectAgent` and
`...GroupMentionSkipsMentionOnlyUntargetedDirectAgent` (pre-existing,
still pass) pin that explicit `@mentions` are unaffected.
`TestResolveDirectRouteApprovalResolutionReachesMentionOnlyIssuerWithoutMention`
pins the approval round trip. `go test ./internal/ws/handler/...
./internal/agentreceive/... ./internal/ws/agentapi/...` passes in full.
