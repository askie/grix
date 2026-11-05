# Agent message edit permission and card restriction

## Context

Rewriting already-delivered content needs independently revocable permission.
The same edit service also settles server-generated approval, binding and
question cards; blindly exposing that ability would let an agent forge a card,
while blocking all card edits would make legitimate settlements duplicate it.

## Decision

- `message.edit` (`agentscope.ScopeMessageEdit`) defaults off and sits next
  to `session.send` in backend/frontend scope lists. The `message_edit` invoke
  action takes `session_id`, `msg_id`, `content` and requires that scope.
- Invoke and raw `edit_msg` use the same `service.EditMessage`. Sender
  ownership still applies (20008); non-text messages or standalone
  `grix://card/...` content are rejected with `ErrMessageEditNotAllowed` /
  20009. A revoked/deleted message is not found by the existing query.
- Only trusted server callers may set service `MessageEditActor.AllowCardMessage`
  and WS `EditMsgPayload.AllowCardMessage`. The latter is tagged `json:"-"`,
  so incoming JSON cannot opt into the bypass. Invoke/raw incoming edits leave
  it false; internal approval/binding/question settlement sets it explicitly.
- Keep the restriction in the shared service, not just the invoke tool:
  otherwise raw packets bypass it. Scope gating described here is the invoke
  action's gate; do not infer another gate from sharing its edit handler.

Tool execution now follows the
[individual-card decision](2026-09-23-tool-execution-cards-stored-individually.md);
its old server group-accumulator implementation is not a reason to retain a
public card-edit bypass.

## Alternatives

- Reuse `session.send` / `agent.dispatch`: editing approved content is a
  distinct owner permission.
- Unconditional service rejection: internal in-place settlement falls back to
  sending a new card, creating a subtle duplicate-message regression.
- A wire-settable bypass: an untrusted connector could forge card/status data.

## Consequences

Future internal card editors must explicitly opt in; omitting it fails closed
but can duplicate cards through existing fallback sends. Keep locale/scope-picker
entries aligned. Verify internal settlement actually edits in place: tests that
only observe eventual success can pass through the duplicate-send fallback.

## Verification

Paths from repository root:

- `backend/internal/api/service/message_edit_service{,_test}.go`: own text,
  other sender, card/non-text, trusted bypass and revoked message.
- `backend/internal/ws/agentapi/{agent_invoke_dispatch.go,agent_invoke_message_edit_test.go,manager.go}`:
  invoke scope/params, shared hook, unreachable JSON bypass.
- Existing approval/binding/question settlement tests must keep their in-place
  behavior; the frontend scope-picker test/locales cover `message.edit` display.
