# Mobile connector administration over WebSocket

## Context

Installing an agent client and creating an agent were desktop-only. The desktop
toolbar (`frontend/lib/modules/system/agent_client_toolbar_view.dart` and
`grix_connector_service.dart`) drives the local connector through
`http://127.0.0.1:<adminPort>/api/install`, `/api/agents` and `/api/probe`, and
the home entry is gated on `PlatformCapability.isDesktop`. A phone cannot reach
that loopback port, so phone users had no way to add an agent to a machine they
already own.

The connector daemon is both the host of that admin API and the receiver of
`local_action` packets, and the backend already has a client → backend →
connector request/response path with cross-node Redis forwarding
(`backend/internal/ws/agentapi/local_action_handler.go`,
`local_action_forward.go`). Reusing it avoids opening any new inbound surface on
the user's machine.

## Decision

### 1. `connector_admin` local action

`action_type = "connector_admin"`, params `{op, args, actor_id}`. `actor_id` is
the requester's user id; the connector logs by it.

Ops and args:

- `list_installable` → result `{platform, agents:[...]}`, same shape as
  `GET /api/install`.
- `install {agent_type}` → accepted asynchronously; returns
  `{agentType, status:"started"}`, or `in_progress` when already installing.
- `install_progress {agent_type}` → same shape as `GET /api/install/:type`;
  a failed install reports `status:"error"`.
- `add_agent {name, ws_url, agent_id, api_key, client_type}` → same semantics as
  `POST /api/agents`.
- `probe {fresh, timeout_ms}` → same shape as `GET /api/probe`.

Success and failure are carried by the result envelope `{ok, result?, error?,
error_code?}`; `local_action_result.status` is always `ok` and must not be used
to decide the outcome. Connector error codes (`unsupported_op`, `forbidden`,
`remote_admin_disabled`, `remote_admin_unavailable`, `invalid_params`,
`internal_error`) pass through to the client unchanged.

Dispatch is gated on the connection's declared `local_actions` containing
`connector_admin`, the same way `apply_relay_state` is gated. A Hermes (Python)
connection on the same host does not declare it and therefore never receives
these commands. An old connector that does not declare it fails the send, and
the backend reports `error_code = "unsupported"`.

### 2. `agent_connector_admin` WebSocket command

Request `{agent_id, op, args}`; response `agent_connector_admin_resp`
`{result?, error?, error_code?}`.

- `agent_id` is an online agent used purely as a channel to reach one host.
- Authorization: the requester must be the agent's owner
  (`agent.OwnerID == userID`). A sharee is always `forbidden`. Dispatch uses the
  owner-scoped route, so a command can never land on a sharee's connection.
- `op = create_agent {agent_name, client_type}` is composed by the backend:
  create the Agent row through the existing owner-scoped service
  (`provider_type = 3`), then send `add_agent` to the channel agent's connector
  with `ws_url` taken from the new agent's API endpoint. If the connector call
  fails, the freshly created Agent row is deleted so no orphan is left. The
  response carries only fields the REST create endpoint already returns.
- Write ops (`install`, `add_agent`, `create_agent`) are audit-logged (with
  secrets stripped) and rate limited per owner (10 per minute).
- The backend waits at most 18s for a connector reply. `install` is accepted
  asynchronously, so it never approaches that bound.

### 3. `host_name` and `supports_connector_admin` on the agent list

`GET /v1/agents/list` (and agent detail) now return `host_name`, read from
`config.host_meta.hostname` exactly as the gateway relay page does, and
`supports_connector_admin`, computed from the same Redis capability snapshot
that backs `state_known`. Empty `host_name` means "unknown host" and is a normal
state for connectors that never reported host metadata.

## Alternatives

- **Expose the connector admin API through a tunnel or public port.** Rejected:
  it opens an inbound administrative surface on the user's machine, and the
  reverse `local_action` channel already exists and is authenticated.
- **Let the phone drive the desktop flow verbatim** (client creates the Agent
  row via REST, then asks the connector to add it). Rejected: two round trips
  through an unreliable mobile link can leave an Agent row that no connector
  ever picks up. Composing `create_agent` server-side keeps the rollback in one
  place.
- **Derive the host name on the client from `config.host_meta`.** Kept only as a
  fallback for older backends; an explicit field keeps the grouping contract in
  one place and lets the server change where host metadata lives.
- **Migrate the desktop toolbar onto the same WebSocket path.** Deferred: the
  desktop path also covers prerequisite checks and synchronous installs that the
  action contract does not model. Only the post-create tail (relay configuration
  and opening the session) and the name prompt are shared today.

## Consequences

- Old connectors keep working: they do not declare `connector_admin`, the send
  fails closed, and the phone shows "upgrade the connector" instead of a generic
  error.
- The channel agent and the requesting client may sit on different nodes; the
  command travels over the existing Redis local-action forwarding, whose 20s
  budget bounds the 18s wait.
- `supports_connector_admin` is only populated by the list and detail endpoints;
  every other response leaves it `false`, which is the safe default because the
  phone picks channels from the list.
- A connector clears its install progress record once an install finishes, so
  `install_progress` can answer `unknown` for a run that actually succeeded. The
  client therefore treats `unknown` as inconclusive and cross-checks
  `list_installable` for `installed` before continuing to poll; polling alone
  would stall until the client's own deadline and report a false timeout.
- The connector side ships in grix-connector 4.8.0
  (`feat/connector-admin-local-action`).

## Verification

- `backend/internal/ws/agentapi/connector_admin_action_test.go`: envelope
  success/failure, `unsupported`, bare result, offline fast-fail, missing-owner
  rejection, timeout settlement.
- `backend/internal/ws/handler/agent_connector_admin_test.go`: invalid payload,
  unknown op, sharee forbidden (with an active share row), offline channel
  agent, write rate limiting, audit-log secret stripping.
- `backend/internal/api/service/agent_service_host_name_test.go`: `host_name`
  for reported, missing and unparsable configs.
- `frontend/test/modules/system/connector_admin_client_test.dart`: the
  `{platform, agents}` envelope, install args, progress fallbacks, `create_agent`
  fields, upgrade-required error codes, `host_name` fallback parsing.
- `frontend/test/modules/ai/agents_view_host_install_test.dart`: host grouping
  including the unknown-host bucket, and the install button's enabled state.
- `frontend/test/modules/system/remote_agent_install_sheet_test.dart`: the type
  list, and both directions of the `unknown` progress case below.
