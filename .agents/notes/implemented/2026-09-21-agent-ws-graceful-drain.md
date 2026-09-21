# Agent WebSocket graceful drain

## Context

A planned WebSocket node shutdown used to close every agent connection immediately. An agent run could therefore lose the socket between its final output fence and `event_result`, making a recoverable rolling restart surface as a customer-visible failure. A new socket cannot safely acknowledge output sent on the old socket because the backend does not persist a cross-connection output-sequence watermark.

## Decision

On planned shutdown, the WS node rejects new agent connections and new initial event dispatches, immediately closes idle connections with 1001, and keeps connections with active runs until their terminal result and ACK are flushed. The first drain call fixes a three-minute wall-clock deadline; it is never renewed. At the deadline the transport is forced closed and existing connector fail-closed behavior applies. The WS StatefulSet termination grace is 240 seconds: 180 seconds of agent drain plus the existing worst-case Redis stop (2s), background seal (5s), stream finalizers (10s), and moderation cleanup (15s), leaving 28 seconds of process-level margin. HTTP shutdown normally overlaps the drain and is not required for this arithmetic to fit.

## Alternatives

- Connector-only recovery across a reconnect was rejected because an ACK on the new socket cannot prove that output on the old socket was accepted; it could silently convert data loss into success.
- An unbounded backend drain was rejected because approvals or hung agents could prevent rollout completion.

## Consequences

Ordinary events arriving during drain use the existing durable offline queue; non-queueing dispatch reports unavailable. Existing ACK retries for already registered runs remain allowed. This change deliberately leaves the existing Redis Pub/Sub at-most-once delivery window unchanged and does not enlarge it. This changes no wire protocol and requires no connector or Hermes synchronization. It covers planned shutdown only, not crashes, process kills, or 1006 disconnects. Runs exceeding three minutes still fail closed.

The WS workload is currently a single-replica StatefulSet, so a planned rollout can keep that replica terminating for up to three minutes while an active run finishes. This temporary rollout unavailability is an explicit operations tradeoff: preserving an in-flight customer result during a planned restart takes priority over fast replacement, while the fixed deadline still bounds rollout completion.

## Verification

Focused lifecycle tests cover idle migration, active-run completion, terminal-ACK flushing, admission rejection and queuing, fixed-deadline force close, idempotence, timer cleanup, and race detection. The rendered Kustomize manifest is checked for the 240-second termination grace.
