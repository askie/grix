# session_activity_set rejections carry a policy code and echo the packet

## Context

`session_activity_set` (agent composing ticks) is fire-and-forget: the backend
sends nothing on success and a `send_nack` on failure. Every handler error was
collapsed into `5001 session activity update failed`, the backend logged
nothing, and the connector dropped unmatched nacks silently. A composing tick
rejected because the agent is muted, the group is muted, the agent is no longer
a session member or the session is gone was therefore indistinguishable from an
internal failure, and group members losing the composing indicator could not be
diagnosed from either side.

## Decision

- `handleSessionActivitySet` maps policy rejections (`agentmsg.ErrPermissionDenied`,
  `sessionguard` denied errors, `gorm.ErrRecordNotFound`) to
  `protocol.CodeUnauthorized` (4003) with the `sessionguard.ErrorMessage` text,
  the same contract the send and stream paths use. Explicit `SendError` values
  pass through; everything else stays `CodeServerInternal` (5001).
- Policy rejections log at info, internal failures at warn.
- `SendNackPayload` gains optional `cmd` and `session_id` fields echoing the
  rejected packet so fire-and-forget senders can attribute a nack without
  tracking outbound seq numbers. Only `session_activity_set` sets them today.
- The connector logs unmatched nacks that carry `cmd` (deduplicated per
  cmd+session+code) and keeps ticking; it does not stop the composing loop on a
  rejection.

## Alternatives

- New dedicated nack codes per reason: rejected, the existing 4003 + message
  convention already distinguishes reasons and avoids a new contract.
- Connector-side seq→cmd tracking: rejected, seq is assigned at flush time and
  success produces no ack, so the table has no reliable lifecycle.
- Stopping the composing tick loop on a 4003: rejected, rejections are not
  permanent within a task (all-members mute lifted, delegate enabled mid-task)
  and continuing to tick lets the indicator recover on its own.

## Consequences

- Additive payload fields; old connectors ignore them, grix-hermes ignores
  unmatched nacks and needs no change.
- A muted or removed agent still costs one rejected tick per 25s per session;
  backend log volume is bounded by that tick rate and stays at info.

## Verification

- `backend/internal/ws/agentapi/packet_handlers_session_activity_nack_test.go`
  covers the code/message mapping and the cmd/session_id echo.
- `grix-connector/tests/aibot-client-unmatched-nack.test.ts` covers connector
  logging, dedup window and the no-cmd/output-packet exclusions.
