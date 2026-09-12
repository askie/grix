# Agent message editing gets its own scope and a hard card-message restriction

## Context

The `edit_msg` WS control frame (`handleEditMsg` -> `service.EditMessage`)
let an agent edit any message it had sent, gated only by "you can only edit
your own message" (`ErrMessageEditDenied`, code 20008). It carried no scope
check at all. A connector-facing MCP tool now needs to expose this as
`grix_message_edit` so an agent acting as a task-flow "team lead" can rewrite
a status message (e.g. a mermaid flowchart) in place as sub-tasks report
back. Because editing can silently rewrite content the owner has already
read and approved, this needed to be a permission the owner can grant or
revoke independently of every other agent capability (`session.send`,
`agent.dispatch`, etc.), not folded into an existing scope.

Separately, the same `EditMessage` service function is also called by
several *internal* server flows that rewrite an agent's own card message in
place as part of normal product behavior: approval-card settlement, binding
card updates, the question-reply-card settle path, and the streaming
tool-execution-group card accumulator. All of these edit a
`[text](grix://card/...)` message — the same shape a malicious or buggy
agent-driven edit could use to forge a fake approval/status card.

## Decision

- Add `agentscope.ScopeMessageEdit` (`message.edit`), default off, ordered
  next to `session.send` in `AllowedScopes()`/the frontend scope picker.
- Add invoke action `message_edit` (`session_id`, `msg_id`, `content`)
  gated by this scope, dispatching to the same `service.EditMessage` the raw
  `edit_msg` packet path already uses (no duplicated edit logic).
- In `service.EditMessage`, reject editing a message that is not
  `model.MsgTypeText` or whose content is a standalone
  `grix://card/...` link (`textutil.IsStandaloneCardMessage`) —
  `ErrMessageEditNotAllowed`, surfaced as ws code `20009`. This applies to
  **both** the invoke action and the raw `edit_msg` packet, since both
  call the same service function.
- To keep the internal card-settlement flows working, add
  `MessageEditActor.AllowCardMessage` (service layer) and
  `agentapi.EditMsgPayload.AllowCardMessage` (ws layer, tagged `json:"-"`).
  Only the ~7 known internal call sites that rewrite their own card in
  place (approval card, binding card x2, question-reply-card settle x2,
  tool-exec accumulator) set this to `true`. The raw incoming `edit_msg`
  packet and the new `message_edit` invoke action leave it at its zero
  value (`false`), so an agent can never set it itself — `json:"-"` makes
  the field unreachable from `json.Unmarshal` on the wire payload.
- Already-revoked messages need no separate check: the existing
  `is_revoked = false` clause in the message lookup query already turns an
  edit attempt on a revoked message into `ErrMessageNotFound`.

## Alternatives

- Reuse `session.send` or `agent.dispatch` for edit permission: rejected —
  the owner asked for editing to be independently revocable, and it has a
  materially different blast radius (rewriting already-delivered content
  vs. sending new content).
- Enforce the card/non-text restriction only in the new `message_edit`
  invoke path (agentapi package), leaving the raw `edit_msg` packet
  unrestricted: rejected — an agent could bypass the restriction entirely
  by sending the raw packet instead of using the scoped tool, defeating the
  point of the restriction.
- Enforce the restriction unconditionally in `service.EditMessage` with no
  bypass: rejected outright — this was tried first and breaks the approval
  card, binding card, question-reply-card, and tool-exec-accumulator
  in-place update flows, all of which legitimately rewrite their own
  `grix://card/...` message. Confirmed by running the existing test suite
  before adding the `AllowCardMessage` bypass: those flows' tests still
  passed only because they already have a "fall back to sending a new
  message" path on edit failure — which would have silently turned every
  in-place card update into a duplicate new message.
- Make the bypass a JSON field on `EditMsgPayload` without `json:"-"`:
  rejected — that field is deserialized directly from an untrusted
  incoming `edit_msg` packet; a plain exported field would let a connector
  simply set `"allow_card_message": true` in its own packet and bypass the
  restriction entirely.

## Consequences

- Any future internal caller that needs to rewrite its own card message in
  place must explicitly set `AllowCardMessage: true`; forgetting it fails
  closed (falls back to the caller's existing "send new message" fallback,
  not a crash), so the failure mode is a UX regression (duplicate message)
  rather than a security hole.
- `agent_scope_controller.dart` and all 11 locale files
  (`frontend/assets/i18n/*.json`) gained an `ai_agent_scope_message_edit(_desc)`
  entry; unrelated pre-existing gaps in that fallback map (e.g.
  `webhook.create` has no frontend fallback entry) were left as-is.

## Verification

`go test ./internal/pkg/agentscope/... ./internal/ws/agentapi/...
./internal/api/service/...` passes, including new tests: `TestEditMessage_
{AgentEditsOwnTextMessageSucceeds,RejectsEditingAnotherSendersMessage,
RejectsEditingCardMessage,AllowCardMessageBypassesCardRestriction,
RejectsEditingRevokedMessage}` and `TestDispatchMessageEdit
{RequiresScope,ValidatesParams,WithScopeInvokesEditHookAndPassesThroughServiceError}`.
Pre-existing card-update tests (approval/binding/question-reply-card
settlement, tool-exec accumulator) were re-run unchanged and still pass,
confirming the `AllowCardMessage` bypass preserves that behavior. Full
`flutter test` passes (2863 tests) including the new
`scopeOptions translates message edit scope` case.
