# Agent scope approval and post-approval resume

## Context

Agent API actions require explicit scopes in `agent_api_scopes`. Connectors surface missing-scope errors to the agent but cannot request owner approval. Group access allowlist approval already wrote access on owner card reply but did not redispatch the blocked trigger message.

## Decision

When a scoped platform action fails for lack of permission, the backend sends a standard `agent_question` card to the owner in the existing access-approval private thread (`request_id` prefix `scope:<agentID>:<scope>`). Deny applies a 24h sticky silence (same window as access deny). Approve appends one scope row idempotently.

Post-approval resume is shared:

- **Scope**: if exactly one in-memory active run exists for the agent at request time, after grant the backend posts an owner-only-visible wake message in that session and pushes a single delegate event to that agent (event_id prefix is the session id).
- **Access**: `pending_pairs` stores the first blocked `trigger_msg_id`; on allow the handler redispatches that message to only the gated agent using persisted group semantics (no second loop-chain increment).

## Alternatives

- Connector-side retry or hang/wait until approval: rejected (protocol unchanged, YAGNI).
- Replace entire scope set on approve: rejected (risky; append-only is idempotent).

## Consequences

Owners may grant scopes from cards without opening admin UI. Agents must not retry immediately after the guided 4003 message. Access redispatch requires Redis pending metadata and WS hub wiring via `InitApprovalResumeBridge`.

## Verification

- `go test ./internal/ws/agentapi/... -run 'Scope|AccessApproval|AccessRedispatch'`
- `go test ./internal/claudeaccess/... ./internal/pkg/agentscope/...`
