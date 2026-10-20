# Owner-identity session send still wakes an explicitly quoted agent

## Context

Dispatch callbacks use `grix_session_send`, which stores the message as the
owner (`sender_type=1`) and stamps `extra.origin_agent_id` with the agent that
actually sent it. Group routing used that id to drop the origin agent from
every wake target. That correctly stops continuation ("the agent who just
spoke") from waking the reporter, but it also dropped an explicit quote of
the reporter's own anchor. When the dispatcher and the worker are the same
agent, the callback quotes that agent's own message and the group never
resumes.

## Decision

An owner-identity group message still skips the origin agent for continuation
and for `@所有人`. It does not skip that agent when the message individually
quotes or `@`-mentions them. A message the agent sends as itself
(`sender_type=2`) still never wakes that same agent. Private chats are
unchanged: they do not apply the origin skip.

## Alternatives

Omitting `origin_agent_id` on every `session_send` would also let the quote
through, and would wake the origin agent for a plain continuation with no
quote. Keeping the stamp and carving out only the explicit quote or `@` keeps
the continuation guard.

## Consequences

A callback written as the owner now resumes the quoted agent even when that
agent sent the callback. Connectors do not implement this rule; only the
group direct-route on the backend does.

## Verification

`TestSessionSendOwnerIdentityDoesNotWakeOriginAgent` covers both sides for a
proprietary Codex-type agent: continuation alone does not wake; quoting that
agent's own anchor does, as `group_mention` / `record_and_process`.
