# aibot-agent-api-v1

The WebSocket protocol an external agent speaks to a Grix backend. An agent that
implements it connects directly, with no grix-connector and no ACP bridge in
between — this is how Hermes is integrated.

Everything below is generated from, and enforced by, code in this repository:

- `internal/ws/protocol/hermes_profile.go` — the declared surface.
- `internal/ws/protocol/testdata/hermes_aibot_agent_api_v1_baseline.json` — the
  frozen snapshot of that surface.
- `internal/ws/protocol/hermes_profile_test.go` — fails the build when the two
  drift apart.
- `internal/ws/agentapi/hermes_contract.go` — the handshake and post-auth checks
  applied to a live connection.

If a behavior is not listed here, it is not part of the contract; do not infer it.

## Versioning

| Field | Value |
| --- | --- |
| `protocol_version` | `aibot-agent-api-v1` |
| `contract_version` | `1` |

## Packet envelope

Every frame is a JSON object with exactly these top-level fields:

```json
{ "cmd": "...", "seq": 1, "payload": { } }
```

## Handshake

The first frame after the socket opens must be `cmd: "auth"`. These payload
fields are required:

| Field | Notes |
| --- | --- |
| `agent_id` | the Grix agent, sent as a string |
| `api_key` | the agent's API key |
| `protocol_version` | must equal `aibot-agent-api-v1` |
| `contract_version` | must equal `1` |

`local_action_v1` must appear in the declared `capabilities`; the handshake is
rejected without it. A rejected handshake answers with `auth_ack` carrying a
non-zero `code` and a `msg` naming the failed check (for example
`protocol_version must be aibot-agent-api-v1`, `contract_version must be 1`,
`local_action_v1 capability required for hermes`).

The server replies with `auth_ack`. On success `code` is `0` and the payload
carries, among other fields, `adapter_id`, `contract_version`,
`supported_capabilities`, `degraded_capabilities` and
`unsupported_capabilities` — the server's classification of what the agent
declared.

## Capabilities

Required — the connection is refused without it:

- `local_action_v1`

Stable — declared and honored by the server; an agent may declare any subset:

- `stream_chunk`
- `session_route`
- `thread_v1`
- `inbound_media_v1`
- `local_action_v1`
- `local_action_result_ack`
- `audit_replay_v2`

A declared capability outside this list is reported back under
`unsupported_capabilities` rather than failing the handshake.

## Public commands

| Command | Direction | Purpose |
| --- | --- | --- |
| `auth` | agent → server | authenticate |
| `auth_ack` | server → agent | authentication result |
| `ping` | both | keepalive request |
| `pong` | both | keepalive response |
| `event_msg` | server → agent | message event |
| `event_ack` | agent → server | message event received |
| `event_result` | agent → server | message event completed |
| `event_stop` | server → agent | stop event |
| `event_cancel` | server → agent | event lifecycle cancel request |
| `queue_clear` | server → agent | event lifecycle queue clear request |
| `queue_reorder` | server → agent | event lifecycle queue reorder request |
| `event_hold` | server → agent | event lifecycle hold request |
| `queue_edit` | server → agent | event lifecycle queue edit request |
| `queue_snapshot_query` | server → agent | event lifecycle queue snapshot query |
| `event_stop_ack` | agent → server | stop event received |
| `event_stop_result` | agent → server | stop event completed |
| `event_state` | agent → server | event lifecycle state update |
| `audit_state` | agent → server | report audit lifecycle |
| `event_cancel_result` | agent → server | event lifecycle cancel result |
| `queue_clear_result` | agent → server | event lifecycle queue clear result |
| `queue_reorder_result` | agent → server | event lifecycle queue reorder result |
| `event_hold_result` | agent → server | event lifecycle hold result |
| `queue_edit_result` | agent → server | event lifecycle queue edit result |
| `queue_snapshot` | agent → server | event lifecycle queue snapshot |
| `event_edit` | server → agent | message edit event |
| `event_revoke` | server → agent | message revoke event |
| `send_msg` | agent → server | send message |
| `client_stream_chunk` | agent → server | stream message chunk |
| `send_ack` | server → agent | send succeeded |
| `send_nack` | server → agent | send failed |
| `edit_msg` | agent → server | edit message |
| `update_binding_card` | agent → server | publish session binding metadata |
| `session_activity_set` | agent → server | session activity update |
| `local_action` | server → agent | local action request |
| `local_action_result` | agent → server | local action result |
| `local_action_ack` | server → agent | local action result received |
| `relay_credential_request` | agent → server | request relay credential |
| `relay_credential_result` | server → agent | relay credential result |
| `relay_state_sync_request` | agent → server | sync relay state |
| `relay_state_sync_result` | server → agent | relay state sync result |
| `relay_state_report` | agent → server | report applied relay state |
| `session_route_bind` | agent → server | bind session route |
| `session_route_resolve` | agent → server | resolve session route |
| `agent_profile_push` | server → agent | agent profile change push |
| `error` | both | generic error |

After a successful `auth`, an agent may send only the commands listed above as
`agent → server` or `both`, minus `auth` itself, plus `agent_invoke` and
`agent_skills_update`. Anything else is rejected.

## Local actions

`local_action` requests the agent run one of these action types; the agent
answers with `local_action_result`:

`exec_approve`, `exec_reject`, `file_list`, `set_model`, `set_provider`,
`get_session_usage`, `get_rate_limits`, `configure_gateway_provider`,
`apply_relay_state`, `skill_upload`, `skill_enable`, `skill_disable`,
`audit_get_manifest`, `audit_list_spans`, `audit_get_content_chunk`.

## Status vocabularies

| Command | Allowed `status` |
| --- | --- |
| `event_result` | `responded`, `failed`, `canceled` |
| `event_stop_result` | `stopped`, `already_finished`, `failed` |
| `local_action_result` | `ok`, `failed`, `unsupported` |

## Error codes

`invalid_local_action`, `unsupported_local_action`, `missing_approval_id`,
`unsupported_decision`, `approval_not_found`, `stop_handler_failed`.

## Field naming

Recommended public field names — reuse these rather than inventing synonyms:

`agent_id`, `session_id`, `event_id`, `msg_id`, `thread_id`,
`route_session_key`, `content`, `quoted_message_id`, `attachments`,
`mention_user_ids`, `status`, `code`, `msg`, `error_code`, `error_msg`.

Field names that must not appear in public payloads:

`chatid`, `req_id`, `markdown`, `stream`, `media_id`, `upload_id`.

## Minimal implementation surface

The smallest command set a working agent implements:

`auth`, `ping`, `pong`, `event_msg`, `event_ack`, `event_result`, `event_stop`,
`event_stop_ack`, `event_stop_result`, `send_msg`, `client_stream_chunk`,
`send_ack`, `send_nack`, `edit_msg`, `update_binding_card`, `local_action`,
`local_action_result`, `local_action_ack`, `relay_credential_request`,
`relay_state_sync_request`, `relay_state_report`, `session_route_bind`,
`session_route_resolve`.

## Not this protocol

A CLI that speaks the Agent Client Protocol does not implement any of the above.
It is launched by grix-connector with `client_type: acp`, and the connector
speaks `aibot-agent-api-v1` to the backend on its behalf.
