# Apple Watch companion for Grix

State: implemented, including independent credentials and recovery. Real-time
watch voice remains deferred. Live Activity has its own
[implemented decision](2026-09-05-live-activity.md).

## Context

The watch clears owner blockers without taking out the phone: approval/deny,
question reply, stop, agent-state glance and a dictated one-shot instruction.
It is a native SwiftUI watchOS target because Flutter has no watch target.
Chat history, markdown, files, groups, settings, billing and watch login are
outside this scope. Only the WS service owns agent connections and can execute
owner actions; the API service supplies reads and credentials.

## Decision

### Actions and cross-client contracts

- Mirrored iOS notifications retain categories `APPROVAL_REQUEST` /
  `AGENT_QUESTION` and identifiers `approve | deny | stop | reply`. The
  notification service extension must preserve `category`, `action_token`
  and `mutable-content`. Their phone response uses `/v1/notify-callback`;
  its short-lived, single-use token is not a general pending-action API.
- The watch calls HTTPS directly, including on cellular/Wi-Fi without the
  phone. No watch WebSocket: foreground polling and mirrored pushes suffice.
  `GET /v1/chat_states/list` is owner-scoped, newest first, capped at 200;
  optional `state=waiting` means approval/question only, other values are 400.
  Rows include session/agent identity, `state`, `task_title`, `last_run_id`,
  `updated_at`, online flag and `agent_provider_type`. Deleted agents are
  omitted; remote-model agents must not look broken because connector presence
  is false for them.
- `POST /v1/owner-action` runs on the WS service with Bearer auth and body
  `{session_id, action, text?}`, action in `approve | deny | stop | reply | send`.
  Enforce caller ownership (403), revocation, password-change invalidation and
  disabled-user checks, matching API middleware despite the separate net/http
  entry point. `send` calls the same `handler.HandleSendMsg` as WebSocket send
  through an identity-bearing detached connection, so it persists and renders
  on the phone; command relay alone would not persist it.
- Approval/deny require `waiting_approval`; reply requires `waiting_question`;
  stop accepts any nonterminal state; quick-send has no waiting-state gate.
  Stale actions return 409. State alone cannot detect a phone-settled approval:
  also check the approval-card index. That resolved-card lookup fails open on
  Redis error (the agent ignores duplicate approval); a missing required
  blocker still makes the action stale.
- `SavePendingOwnerBlocker` stores the latest blocker in
  `im:agent_api:pending_owner_blocker:<owner>:<session>` for 48h:
  `{kind, agent_id, approval_command_id | question_id + question_message_id,
  run_id}`. It is written where notifications and waiting states are published;
  one current blocker per session replaces the older one. No schema change.
- Rate buckets are independent: `send` gets 30/5min (`owner-action-send`),
  blocker actions share 10/5min (`owner-action`). Chatting must not consume the
  budget needed to approve a task.

`chat_states` names are shared by Flutter, connector MCP and watch:
`running | waiting_approval | waiting_question | completed | failed | idle`.
Change all consumers together. Keep parity tests between notify-token and
owner-action claims/execution; the shared `executeNotifyAction` path must not
fork its approval semantics.

### Independent credential family and recovery

- The phone calls authenticated `POST /v1/auth/watch/issue` (10/user/minute)
  with its access token to mint a **watch** access/refresh pair. Its own refresh
  token never leaves the phone. The ordinary refresh tables are reused:
  `platform="watch"`, deterministic `device_id="watch:<user_id>"`, existing
  live `(user_id, device_id)` uniqueness. Platform is authorization/revocation
  identity, not just a label.
- Reissue transactionally revokes all live watch sessions/families before minting
  a new one, then invalidates superseded access tokens after commit. Watch and
  phone rotate independently through `/v1/auth/refresh`. Watch renewal is
  single-flight, proactive within 5 minutes of expiry and reactive after 401;
  a 5xx is a server error, not evidence that credentials must be reissued.
- Login/register/QR/account switch issue via `ensureWatchCredentials`; normal
  phone refresh does **not** reissue, which would revoke a healthy out-of-range
  watch. `_issueAndSyncWatchCredentials` explicitly supplies the just-granted
  Bearer token and does not call `ensureTokenFresh` inside login. Drop a result
  if the phone account/token changed while issuing; failure does not block login.
- Cold start, phone activation and watch-install changes check locally for
  `isPaired && isWatchAppInstalled` and missing previously pushed credentials.
  The bidirectional `pub.dhf.grix/watch_session` channel uses native→Dart
  `ensureCredentials` and Dart→native `checkCredentialsState` after handler
  registration. Missing peer methods are tolerated for version skew.
- A watch with no/expired refresh credentials requests
  `{"request":"request_credentials"}` via `sendMessage`, or queued
  `transferUserInfo` when unreachable. Both sides throttle requests/issues to
  60s; repeated issue would revoke the credential just given. A logged-out phone
  replies with empty credentials. Older peers can ignore the additive request.
  Credential observation reloads the watch automatically; generation checks
  keep an older in-flight failure from replacing newly received credentials.
- Password reset/full logout revoke every family; session-scoped phone logout
  also revokes watch families. Device-list kick remains device-scoped: the
  watch is its own revocable `platform=watch` entry.

WatchConnectivity sends watch `access_token`, `refresh_token`, API/WS URLs and
expiry through `updateApplicationContext`. Native bridge, Dart sync and watch
provider share those keys; the watch keychain tolerates old access-only records.
The credential stays in the watch app keychain, never the widget shared group.

### UI and packaging

Inbox shows waiting sessions/actions; Agents shows state/presence; quick-send
uses an agent's latest session and displays the last plain-text reply.
`AVSpeechSynthesizer` reads only on user tap, follows system language and uses
`.playback` / `.spokenAudio`, deactivating when finished. The composer polls
12 times at 5s for a reply and cancels on disappearance. This supplies dictation
and readback without a real-time voice transport. The original voice constraint
was the absence of a LiveKit watchOS SDK; a future real-time path requires a
separate transport decision, not an assumption that phone voice works on watch.

`GrixWatch` (`pub.dhf.grix.watchkitapp`) and `GrixWatchWidget`
(`pub.dhf.grix.watchkitapp.widget`) target watchOS 10. Their
`frontend/ios/Flutter/Watch.xcconfig` includes `Generated.xcconfig` for app
version/build, deliberately excluding iOS Pods search paths. Runner depends on
and embeds the watch, which embeds the widget; the ordinary Runner archive
includes both. The app writes only pending/running counts to
`group.pub.dhf.grix.watch` and reloads widget timelines; the widget has no
network access. Signed builds require both bundle IDs and App Group provisioning;
first App Store submission also needs watch screenshots. Include native targets
in build and dSYM handling.

## Alternatives

- Relay all traffic through the phone: rejected; loses the out-of-range case.
- Share the phone refresh token: rejected; rotation/reuse revokes the family.
- Send access token only: superseded; fails after the phone token expires.
- Reissue on every phone refresh: rejected; invalidates a healthy watch while
  its replacement may still be queued. On-demand recovery is narrowly triggered.
- Mint notification action tokens on demand: rejected; they represent a past
  single-use event, while Inbox acts on current server state.
- A background watch WebSocket or real-time voice: deferred beyond this scope.

## Consequences

The WS service has a second authenticated owner-action entry; its authorization
and rate policies must stay aligned with notify actions and API authentication.
Credentials are real independent login sessions. Native target provisioning and
physical-device checks remain release conditions, not established by simulator
builds. No new watch-specific schema is needed.

## Verification

Current enforcement and test paths from repository root:

- `backend/internal/ws/owner_action{,_test}.go`: identity, per-action staleness,
  independent budgets, persistence and notification-path parity.
- `backend/internal/api/{handler/chat_state_test.go,service/chat_state_service.go}`:
  owner-only read, filtering, provider type and deleted-agent omission.
- `backend/internal/api/service/{auth_watch_token_service_test.go,auth_service.go}`:
  family isolation, rotation, reissue revocation and phone logout/password reset.
- `frontend/test/data/providers/auth_service_watch_credentials_test.dart` and
  `frontend/lib/data/providers/auth_service_payload.dart`: explicit auth,
  logged-out recovery, throttle and account-change rejection.
- `frontend/ios/{Runner/WatchSessionBridge.swift,GrixWatch/WatchCredentials.swift,GrixWatch/WatchStore.swift}`:
  recovery delivery, keychain, single-flight renewal and credential generation.

Still require a paired device/signed build: mirrored approval/deny/stop/reply
with locked iPhone and unlocked watch; out-of-range Inbox/action; phone-settled
item removal; matching complication counts; independent renewal beyond the
phone access TTL (previously 2h), phone-logout revocation; speaker readback/stop;
and Runner archive containing
`Watch/GrixWatch.app/PlugIns/GrixWatchWidget.appex` under real signing.
