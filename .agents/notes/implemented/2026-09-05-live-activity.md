# Live Activity for agent runs

State: implemented. Complements the
[Apple Watch companion](2026-09-05-apple-watch-companion.md).

## Context

Durable `chat_states` transitions already drive notifications, but owners need
a persistent lock-screen/Dynamic Island glance. Server push-to-start works
while the app is closed; short runs need debounce to avoid flicker. Notification
and watch actions already cover approvals, so interactive ActivityKit buttons
are deferred.

## Decision

### Lifecycle and gating

One card per session run; at most three live sessions per user, ending the
oldest when a fourth starts. Tap opens `grix://session/<id>` through the
existing push-tap navigation. Phases are
`running | waiting_approval | waiting_question | completed | failed | stopped`.

| Transition | Frame and priority |
| --- | --- |
| Running survives 10s | `start`, priority 5; recheck current nonterminal state and open at its actual phase |
| Waiting with no card | Immediate `start` at waiting phase with alert, priority 10 |
| Existing card enters waiting | `update` with alert, priority 10 |
| Owner resolves approval/question | `update` to running, priority 5 |
| Task title changes | `update`, priority 5; coalesce per session over 5s |
| Terminal/stop | `end`, priority 10; dismiss after 5min |
| Stale-running settlement | `end`, priority 10 (all end frames are high priority) |

The 10s timer belongs to the WS node; losing it on restart may omit a card and
is accepted. Waiting bypasses debounce because an owner blocker is not a short
run. `chat_states` has no waiting→running write for action resolution, so
successful `executeNotifyAction` emits the resume frame. In-app approval and
question answers also hook their WS resolution paths (including Hermes rewrite
and offline text fallback); a new run with a live card updates instead of
starting another. Title notification lives in `internal/liveactivity` so both
API rename and WS lifecycle paths can call it.

Start requires at least one enabled push lifecycle preference
(`approval_requested`, `agent_question`, `task_completed`, `task_failed`,
`task_stopped_unexpected`) via `ResolvePref`/`HasChannel(push)`, plus a device
push-to-start token. Evaluate the preference gate only at start: existing cards
continue to receive updates/end when user preferences later change, to avoid
zombies. The worker still honors the deployment-wide iOS push-channel switch.

### Tokens, transport and ownership

- `POST /v1/devices/bind` optionally reports the per-device
  `live_activity_token`; additive migration
  `backend/migration/123_devices_live_activity_token.sql` adds the nullable
  VARCHAR(512) column. Active iOS devices with it receive starts.
- Authenticated `POST /v1/live_activities/token` accepts
  `{session_id, activity_id, token, device_id}`. Owner scope applies; another
  user's or unknown session returns 403, not 404. Redis
  `im:la:tokens:<user_id>:<session_id>` maps device ID to activity ID/token
  with 24h TTL; updates/end use these tokens. End deletes the key after send.
- Registering/rotating an activity token immediately sends the current state:
  update if live, end if already terminal, **without alert**. Otherwise a
  transition between start and token report could be lost forever; alerts on
  every token report would repeatedly buzz.
- `im:la:sessions:<user_id>` maps session to start time with 24h TTL. The
  lifecycle side owns this index/cap; push delivery owns activity tokens. No
  permanent activity table or scan of Redis keys is needed.
- Invalid APNs activity tokens (`BadDeviceToken`, `Unregistered`, `ExpiredToken`,
  HTTP 410) clear only the start-token column or activity hash field. They do
  **not** deactivate the device's ordinary notification channel.

Lifecycle publishes `pushTask{cmd:"live_activity"}` through existing NATS
`im.push.offline.<user_id>`. Payload fields are `event` (`start|update|end`),
`session_id`, static `attributes{session_id,agent_id,agent_name}`,
`content_state{phase,title,detail,updated_at_ms}`, optional `alert{title,body}`
and `dismissal_at_ms`. APNs uses `apns-push-type: liveactivity`, topic
`<bundle>.push-type.liveactivity`, and `aps` fields `timestamp`, `event`,
`content-state`; start adds `attributes-type:"GrixRunAttributes"` and
`attributes`, end adds `dismissal-date`. Old push workers ACK an unknown cmd
without sending, so rolling deployment must include the capable worker.

### Native client and packaging

`GrixActivity` (`pub.dhf.grix.GrixActivity`) is an embedded iOS WidgetKit
extension with lock-screen/Dynamic Island views and `.small` supplemental
family for watchOS 11+ Smart Stack. No `GrixWatch` code is required for that
card. **Extension minimum is iOS 18**, despite push-to-start existing at 17.2:
`supplementalActivityFamilies` requires 18 and conditional duplicate
`ActivityConfiguration`s for one attributes type would be ambiguous. Earlier
iOS versions retain ordinary notifications; Runner's lower minimum is unchanged.

`frontend/ios/GrixActivity/GrixRunAttributes.swift` is shared with Runner.
`ContentState.phase` is a string, with unknown values falling back to running
style, rather than a decoding enum that could freeze the card. The extension
has no app i18n; backend `notification.LiveActivityPhaseCopy` supplies all
recipient-language text, while symbols/tints convey phase. State renames must
update backend producers and native rendering together. Runner declares
`NSSupportsLiveActivities` and the `grix` URL scheme.

`LiveActivityBridge.swift` observes push-to-start and per-activity token updates,
including existing activities. It buffers reports until Flutter drains
`pub.dhf.grix/live_activity` with `drainPending`. Flutter owns authenticated
device bind/token POST; native does not call the backend. Reports are
fire-and-forget, never awaited inside login (the auth interceptor could wait
on itself). Foreground uses the same push path.

## Alternatives

- App-observed start: rejected; the app is often backgrounded/closed.
- Permanent activity table: rejected for short-lived replaceable tokens.
- Approve/deny App Intents: deferred; needs native credential handling and
  another owner-action client, while notifications/watch already cover it.
- Immediate start on running: rejected for short-run flicker.

## Consequences

The cross-service state contract now includes WS/API → push → native extension.
Push volume increases; priority 5 running updates can coalesce. Signed release
requires the registered extension bundle ID and its inclusion in Runner's
archive; existing APNs credentials/topic prefix are reused. No card is expected
without a start token, with Live Activities disabled or below iOS 18.

## Verification

Current paths from repository root:

- `backend/internal/liveactivity/notifier_test.go`: debounce, immediate waiting,
  cap/eviction, preference gate, coalescing, idempotent end and alert-free catch-up.
- `backend/internal/push/{live_activity_test.go,provider/apns_live_activity_test.go}`:
  recipient/token choice, invalid-token cleanup without device retirement,
  end cleanup, unknown cmd and APNs header/body contract.
- `backend/internal/ws/agentapi/live_activity_resume_test.go`: in-app approval/
  question restore running only for a live card.
- `backend/internal/api/handler/live_activity_test.go`: owner scope, TTL,
  current-state catch-up.
- `frontend/ios/{Runner/LiveActivityBridge.swift,GrixActivity/GrixRunAttributes.swift,GrixActivity/GrixRunActivityWidget.swift}`
  and `frontend/ios/Runner.xcodeproj/project.pbxproj`: buffering, wire decoding,
  Smart Stack family, minimum OS and embed target.

Still needs signed device/sandbox APNs verification on iOS 18+ with watchOS 11+:
long run starts on lock screen/Smart Stack; waiting alerts; watch and in-app
actions restore running; terminal shows outcome then dismisses after 5min.
Simulator/build success alone cannot establish push delivery or provisioning.
