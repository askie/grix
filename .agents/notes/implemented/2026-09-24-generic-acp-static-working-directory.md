# Generic `acp`: static working directory, not owner-bound

## Context

Every grix-connector CLI client binds a working directory per chat session: the
connector defers the first inbound message, the owner answers an
`agent_open_session` card or runs `/grix open <dir>`, and only then does the run
start. That flow assumes the owner knows the CLI and wants to point it at one of
their projects.

`client_type: "acp"` (see `2026-09-05-generic-acp-client-type.md`) exists for a
different audience: an external developer connecting their own ACP-speaking
agent. Requiring a directory there was wrong in three ways.

- The platform has no card for it. Generic `acp` has no `inbound_cards.go`, so
  the connector's `Session binding missing.` notice reached the owner as bare
  English text with no way to act on it — the first message to a freshly
  connected agent dead-ended.
- The agent may have nothing to do with a project directory at all.
- The toolbar gated itself on the binding, so an agent that never bound showed
  no toolbar and no way out.

ACP's `session/new` requires an absolute `cwd`, so the directory cannot simply
be dropped. It has to come from somewhere the owner is not asked about.

## Decision

For `client_type: "acp"` only, the working directory is supplied by
configuration and is not owner-changeable.

- The connector agent entry takes an optional `cwd`. `src/bridge/cwd-policy.ts`
  resolves it, falling back to the home directory.
- `acp` joins `hermes` in the workspace-free set: the first inbound message
  auto-binds instead of being deferred, so nothing prompts for a directory.
  Unlike `hermes`, `acp` is also *cwd-locked*: the persisted binding is only a
  cache of the configured value, so a binding that disagrees with the config is
  overwritten. That is what makes editing `cwd` take effect — the binding store
  survives restarts, and `replaceInstance` already destroys the adapter pool on
  a config change, so nothing here needs to touch slots.
- `/grix open`, the `agent_open_session` card submission and `session_control
  unbind` are rejected for cwd-locked clients on both the text-command and
  local-action paths, with one message naming the config field to edit.
- The backend toolbar package drops its session-binding gate and its entire
  `session_control` item (status / stop / unbind / usage), and sets
  `OmitListSessionsButton`. The toolbar is now invisible while idle and carries
  only stop-output, plus model/mode selectors once the connector reports them.
- The Flutter quick-bind panel adds `acp` to
  `kDirectoryBindExemptAgentClientTypes`.

Scope boundary that matters more than anything else here: all of this keys on
**client_type**, never on adapter type. `gemini`, `kimi`, `qwen`, `kiro`,
`copilot`, `agy`, `reasonix`, `qodercli`, `mcode`, `dim`, `codebuddy` and
`zeroclaw` all run on `adapterType === "acp"` and keep the per-session binding
flow unchanged. `BINDING_REQUIRED_ADAPTERS` is deliberately untouched.

## Alternatives

- **Disable session binding for `acp` (`enableSessionBinding: false`).** The
  binding store is what gives each chat session its own ACP session in the
  adapter pool; turning it off would collapse every chat onto one CLI session.
- **Keep `/grix open` as an optional override.** Rejected by the owner: the
  directory is meant to be fixed by whoever deploys the agent, and an override
  reintroduces the state where the toolbar and the card have to explain binding.
- **Hide the toolbar unconditionally.** Leaves a runaway external agent with no
  stop button. `Visible: len(items) > 0` gets the same empty toolbar while idle
  without losing `session/cancel`.
- **Block `session_control open` on the local-action path too.** That path also
  carries `grix_dispatch_agent`'s server-issued bind, so a blanket rejection
  would break agent-to-agent dispatch to an `acp` agent. Only `unbind` is
  rejected there; the toolbar entry that a human could have used is gone.

## Consequences

- Rolling this out needs the connector first (an old server with a new connector
  merely shows one stale toolbar item), then the server, then the clients.
- Editing `cwd` takes effect on the next inbound message after the connector
  reloads: the instance is rebuilt, and the stale binding is overwritten.
- A `cwd` that cannot be resolved falls back to the home directory with an error
  log naming the value. Falling back to the "bind a directory" prompt instead
  would strand the session permanently, because `open` is rejected for this
  client type.
- `grix_dispatch_agent` still requires and passes a `cwd` for `acp` targets;
  that bind wins over the static value for the dispatched session, unchanged.

## Verification

- `tests/bridge-cwd-policy.test.ts` (connector): the two policy sets by
  client_type including the "ACP-family vendor CLIs are untouched" case,
  static-cwd resolution, auto-bind / overwrite-on-config-change / unusable-cwd
  fallback / hermes keeps its binding, and the open+unbind rejections with a
  gemini pass-through.
- `backend/internal/agenttoolbar/acp_package_test.go`: hidden while idle, stop
  visible without a binding, no `session_control`, `OmitListSessionsButton` set,
  and a rejection for stale `session_control` / `get_session_usage` clicks.
- `frontend/test/modules/chat/services/chat_recent_bind_directory_store_test.dart`:
  `acp` exempt, `gemini` / `kimi` / `qwen` / `kiro` still offered.
