# Two agent receive modes

## Context

Group agents exposed three receive-mode values. Normal mode required a public trigger, legacy ModeAll accepted every routed public message, and mention-only required an explicit target. The distinction between normal and ModeAll was difficult to explain, while private-to-group conversion continued to create ModeAll members.

## Decision

Expose only two behaviors:

- Normal (`1`) accepts both messages directed at the agent and public messages that have no explicit member target.
- Mention-only (`3`) accepts only directed messages, retaining the existing quote, direct-continuation, and approval exceptions.

Legacy ModeAll (`2`) is normalized to normal on read. New writes, including private-to-group conversion, use only `1` or `3`. Explicitly targeted messages keep their existing routing and are not broadcast to non-target normal agents. Public-message direct/delegate deduplication applies whenever the same visible agent occupies both roles, not only during cold start, and preserves `visible_to` restrictions.

## Alternatives

A database migration from `2` to `1` was rejected for this change. Compatibility normalization avoids adding a migration and avoids changing legacy ModeAll behavior on old backends during a rolling deployment. Keeping three user-facing modes was rejected because the middle behavior was harder to explain than the two-state model.

## Consequences

Normal agents receive more public messages than before. Mention-only behavior and explicit-target routing remain unchanged. Existing rows may still contain `2`, but backend detail responses and current clients present them as normal. No database migration is required.

## Verification

- Agent receive unit tests cover normal dispatch, legacy normalization, and mention-only rejection of public messages.
- Direct and delegate route tests cover public processing, mention-only filtering, targeted routing, proprietary agents, and duplicate-path suppression.
- Frontend tests cover display normalization of legacy value `2`.
