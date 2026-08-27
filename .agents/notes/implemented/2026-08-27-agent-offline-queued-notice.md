# Agent-offline queued-message notice

## Context

Sending a message to an Agent API type agent (`provider_type=3`) with no reachable
connector connection used to succeed silently. `pushDelegateEvent` tries local dispatch,
then cross-node forward, then falls back to the Redis retry queue
(`im:agent_api:queued_events:<agent_id>`) and returns `true` for that queued case — the
caller (and everything upstream of it) treats "queued" the same as "delivered", so no
error ever surfaces. A user who creates a brand-new agent, sends a message before ever
starting its connector, gets no feedback at all: no error toast, no chat message, nothing.
Confirmed against production data for a real customer session (agent created, connector
never connected once, single message sat in the Redis queue with zero client-visible
trace).

## Decision

- New `protocol.AgentDeliveryCodeQueuedOffline` ("agent_api_queued_offline"), reported with
  `AgentDeliveryStatusQueued` (not `Failed` — this is not a delivery failure).
- `notifyAgentQueuedOffline` (`send_msg_delegate.go`) broadcasts that status and, via the
  existing `EmitAgentDeliveryFailureMessage` path, writes a normal chat message from the
  agent explaining the message is saved and will be processed once the connector is
  running. Reuses the same message/unread/inbox-seq/offline-push plumbing as the existing
  delivery-failure notice — no new notification mechanism.
- Hooked only into `direct_session_route.go`'s primary dispatch (1:1 chats, and agent
  members dispatched directly in a group) by comparing an availability snapshot taken
  *before* `PushDelegateEvent` against the push outcome: unavailable-before + push-succeeds
  (i.e. queued) → notify. A message that dispatches or forwards successfully never triggers it, and a
  message that fails outright still goes through the pre-existing `channel_unavailable`
  failure path untouched.
- Availability is checked with a new `wsagentapi.IsAgentChannelAvailableForOwner(agentID,
  ownerID)`, not the existing agent-level `IsAgentChannelAvailable`. Actual delivery routes
  by the precise `(agentID, ownerID)` connection/route (`lookupConnForDelegate`,
  `loadAgentRouteForOwner`) to keep a shared agent's per-user connections isolated; an
  agent-level check would misclassify a shared agent whose primary connection is up but
  the specific owner's own connection is down (false "available", no notice for an event
  that actually queues) and the mirror case where the primary route key has expired but the
  sender's own per-owner route is still live elsewhere (false "offline" notice for an event
  that actually delivers).
- Repeat notices are suppressed with a Redis `SetNX` cooldown, 10 minutes, keyed by
  `(ownerID, agentID)` only — not `sessionID`. One offline stretch is one fact regardless of
  how many sessions the owner has with that agent; deduping only within a session would
  let a burst across several sessions produce several near-simultaneous notices for the
  same underlying cause.
- The proprietary "delegate" dispatch path (`send_msg_delegate.go`, a human member
  delegating a *different* agent to answer on their behalf, including widget customer
  support) has the identical silent-queue behavior and is intentionally left unfixed here.
  The recipient there is the delegated-to party, not the agent's owner — they typically
  have no way to know or fix which connector needs starting, so surfacing the same notice
  to them would be actionable to nobody. Needs its own design (e.g., notify the owner
  instead of the delegate's counterpart) rather than reusing this hook.
- No attempt to distinguish "this agent has never connected" from "connected before, now
  offline" in the notice text: `direct_session_route.go` loads agents through a narrow
  projection (`directSessionAgentRow`) that does not carry
  `agents.connector_version_seen_at`, and adding that column to the hot path's query for a
  cosmetic text difference was not worth the extra read.

## Alternatives

- Checking availability with the existing agent-level `IsAgentChannelAvailable` was rejected
  after code review: it does not match how `pushDelegateEvent` actually routes, and using it
  would reintroduce the exact false-positive ("先提示离线、稍后队列 drain 又有回复的误报")
  the original code comments were written to avoid, plus a new false-negative for shared
  agents.
- Hooking into the queue layer itself (`delegate_queue.go`'s `enqueueDelegateEvent`) was
  rejected: that package has no `HubInterface`/DB session context, so notifying from there
  would need a new callback plumbed through `agentapi`, a larger change for the same result
  the upper-layer handlers can already produce with the availability snapshot they compute
  anyway.
- A queue-length-edge-triggered notice ("only when the queue goes from empty to non-empty")
  was considered instead of the cooldown key, but its semantics differ from a fixed cooldown
  in ways that need product confirmation (a queue drained and refilled quickly re-notifies);
  the `SetNX` cooldown pattern already has precedent in this codebase
  (`agent_invoke_dispatch.go`'s `callOwnerCooldown`) and is simpler to reason about.

## Consequences

- Older clients that do not recognize `agent_api_queued_offline` degrade gracefully: the
  `agent_delivery_status` payload is additive and ignorable, and the notice is a plain chat
  message like any other agent message.
- A route key can lag its connection by up to the heartbeat-based lease TTL after a
  disconnect; a message sent inside that window is not classified as queued-offline (it is
  still treated as available) even though it may end up queued. This is a narrow, transient
  false negative bounded by the existing lease TTL, not a correctness bug introduced here.

## Verification

- `backend/internal/ws/handler/agent_delivery_notice_test.go`: cooldown suppresses a repeat
  notice; the new code's copy resolves and is present for all 11 languages.
- `backend/internal/ws/handler/send_msg_test.go`: direct 1:1 send to an offline agent gets
  exactly one queued-offline notice (`TestHandleSendMsgDirectAPIAgentDropQueuesEventWithOfflineNotice`);
  a group mention and a group broadcast to an offline API agent member get the same
  (`TestHandleSendMsgGroupMentionTargetsAPIAgentByAgentName`,
  `TestHandleSendMsgGroupWithoutMentionTriggersAPIAgent`); a reachable agent (forwarded to
  another node) gets none (`TestHandleSendMsgDirectAPIAgentAvailableSkipsOfflineNotice`).
- `backend/internal/ws/handler/retry_msg_test.go`: retrying a message to a still-offline
  agent re-triggers the same notice without duplicating the retried message itself.
- `go test ./backend/internal/ws/...` passes in full.
