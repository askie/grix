# Live Activity for agent runs

State: proposed. Builds on the Apple Watch companion note (`implemented/2026-09-05-apple-watch-companion.md`).

## Context

An agent run has a small, fast-changing state that the owner wants to glance at: running, waiting for approval, waiting for an answer, finished, failed. Today that state reaches the phone only as discrete notifications. iOS Live Activities put one persistent, updating card on the lock screen and in the Dynamic Island, and on watchOS 11+ the same activity appears in the watch Smart Stack with no watch code.

Existing pieces this reuses:

- Run state is durable in `chat_states` (`SessionAgentState`), written from the ws service: `UpsertSessionAgentStateRunning*`, `SetSessionAgentStateWaiting`, `UpsertSessionAgentStateTerminal` / `SettleSessionAgentStateByRun` (`backend/internal/store/session_agent_state_store.go`).
- Push delivery: ws publishes `pushTask{user_id, cmd, payload}` on NATS `im.push.offline.<user_id>`; the push worker fans out per device (`backend/internal/push/worker.go`). APNs sending, token-based auth, sandbox/production selection and invalid-token cleanup live in `backend/internal/push/provider/apns.go`.
- Device rows: `model.Device{user_id, platform, push_env, device_token, device_id, is_active}` bound through `POST /v1/devices/bind`.
- iOS already handles a push tap that names a `session_id` (`AppDelegate.notifyPushTap`) and forwards it to Flutter to open the chat.

Constraints:

- Push-to-start needs iOS 17.2+. Updating/ending an activity needs a per-activity push token that only the device can produce after the activity exists.
- Approval buttons inside a Live Activity run as App Intents in the app process and would need the app's access token; the notification buttons already cover approve/deny/stop/reply. Interactive buttons are therefore out of scope for this phase.
- Short runs would flicker a card on and off the lock screen.

## Decision

### Scope

One Live Activity per session run, with three visual phases: running, waiting (approval or question), finished (completed / failed / stopped). Tap opens the session in the app. No buttons on the card in this phase. At most 3 live activities per user; starting a fourth ends the oldest.

### Lifecycle (server-driven, from `chat_states` transitions)

| Transition | ActivityKit event | Priority | Notes |
|---|---|---|---|
| run becomes `running` and is still running after 10s | `start` (push-to-start) | 5 | 10s debounce in the ws node that owns the run, via `time.AfterFunc`; re-check state before publishing. A lost timer on node restart means no card, which is acceptable. |
| `running` → `waiting_approval` / `waiting_question` | `update` with `alert` | 10 | Card switches to the waiting phase with `task_title`. |
| waiting → `running` (owner acted) | `update` | 5 | |
| `task_title` changes | `update` | 5 | Coalesce: at most one update per session per 5s. |
| terminal (`completed`, `failed`, stop) | `end` with `dismissal-date = now + 5min` | 10 | Card shows the outcome, then leaves the lock screen. |
| stale-running settle (`SettleStaleRunningSessionAgentState`) | `end` | 5 | Cleans up cards whose run died silently. |

Gating: no new preference. Send only when the user has the push channel enabled for agent notifications (same check as `dispatcher.dispatchPush`) and the device has reported a push-to-start token, which the device only does when the user has Live Activities enabled in iOS Settings.

### Tokens

- **Push-to-start token** (per device): reported through the existing `POST /v1/devices/bind` with a new optional field `live_activity_token`. Stored in a new nullable column `devices.live_activity_token` (VARCHAR 512). Migration adds the column; no other schema change.
- **Per-activity update token** (per device per activity): the iOS app reports it through a new authed endpoint `POST /v1/live_activities/token` with `{session_id, activity_id, token, device_id}`. Stored in Redis, key `im:la:tokens:<user_id>:<session_id>`, hash `device_id → {activity_id, token}`, TTL 24h. Activities are ephemeral; a table is unnecessary. `end` deletes the key after sending.
- Invalid tokens (`BadDeviceToken`, `Unregistered`, `ExpiredToken`) clear the stored token, mirroring the existing invalid-device-token path.

### Transport

The ws service publishes `pushTask{cmd: "live_activity"}` on the existing `im.push.offline.<user_id>` subject with payload:

```json
{
  "event": "start|update|end",
  "session_id": "…",
  "attributes": {"session_id": "…", "agent_id": "…", "agent_name": "…"},
  "content_state": {"phase": "running|waiting_approval|waiting_question|completed|failed|stopped", "title": "…", "detail": "…", "updated_at_ms": 0},
  "alert": {"title": "…", "body": "…"},
  "dismissal_at_ms": 0
}
```

The push worker resolves recipients: `start` fans out to every active iOS device of the user that has a push-to-start token; `update` / `end` go to the activity tokens in Redis for that session. The APNs provider gains `SendLiveActivity` that sets `apns-push-type: liveactivity`, `apns-topic: <bundle>.push-type.liveactivity`, and builds the `aps` body (`timestamp`, `event`, `content-state`, and for `start` also `attributes-type: "GrixRunAttributes"` and `attributes`; `alert` when present; `dismissal-date` for `end`). Old push workers that receive the new `cmd` during a rolling deploy log and ack it.

### iOS

- New WidgetKit extension target `GrixActivity` (iOS) with `ActivityConfiguration(for: GrixRunAttributes.self)`: lock-screen banner, Dynamic Island compact / minimal / expanded, and `.supplementalActivityFamilies([.small])` so watchOS 11+ shows it in the Smart Stack. `widgetURL` carries the session id; the app's existing URL handling opens the chat.
- `GrixRunAttributes` (shared Swift file between Runner and the extension): static `session_id`, `agent_id`, `agent_name`; `ContentState` = `phase`, `title`, `detail`, `updated_at_ms`. Phases are the `chat_states` names plus `stopped`.
- `Runner/Info.plist`: `NSSupportsLiveActivities = YES`.
- Native `LiveActivityBridge.swift` observes `Activity<GrixRunAttributes>.pushToStartTokenUpdates` and, for every activity, `pushTokenUpdates`, and hands tokens to Flutter over a MethodChannel `pub.dhf.grix/live_activity`. Flutter sends the push-to-start token with the next device bind and posts activity tokens to `/v1/live_activities/token`. Flutter owns auth; native never calls the backend directly.
- When the app is in the foreground it does nothing special: pushes still drive the card, which keeps one code path.

### Watch

No change to `GrixWatch`. The Smart Stack card is rendered by the iOS extension's `.small` family view.

## Alternatives

- **Start the activity from the app when it observes a run starting.** Rejected: the phone app is usually backgrounded or closed when the user wants a lock-screen card; push-to-start is the only reliable trigger.
- **A `live_activities` table.** Rejected: per-activity tokens live at most a day and are replaced on every new run; Redis with TTL is simpler and needs no migration.
- **Approve/deny buttons on the card.** Deferred: App Intents would need the app's access token in native code and a second owner-action client; the notification buttons and the watch app already cover it.
- **Start immediately on `running`.** Rejected: sub-10-second runs (most tool-call round trips) would flash a card on and off.

## Consequences

- One migration (`devices.live_activity_token`), one new authed endpoint, one new push task cmd, one new APNs send path, and one new iOS extension target. The iOS release must include the extension; the same APNs key and topic prefix work for `liveactivity` pushes.
- Push volume rises by a few pushes per run; priority 5 for running updates keeps them coalesced by APNs.
- `chat_states` transitions become a second cross-service contract (ws → push) in addition to notifications; a renamed state must be reflected in the `phase` enum on iOS.
- iOS < 17.2 and devices without Live Activities enabled receive nothing and see no error.

## Verification

- Push worker unit tests: `start` only targets devices with a push-to-start token; `update`/`end` use the Redis activity tokens; invalid-token responses clear stored tokens; unknown `cmd` is acked and logged.
- APNs provider tests: `SendLiveActivity` sets the three headers and produces the documented `aps` shape for `start`, `update`, and `end`.
- ws tests: the 10s debounce does not publish `start` when the run reached a terminal state within the window; a `waiting_*` transition publishes exactly one `update` with `alert`; terminal publishes `end` and clears the token key.
- Handler tests: `/v1/live_activities/token` rejects other users' sessions (403) and stores under the caller's key.
- Manual with the sandbox APNs environment on an iPhone (iOS 17.2+) and a paired watch (watchOS 11+): run an agent task longer than 10s → card appears on the lock screen and in the Smart Stack; approval request → card switches to waiting with an alert; approve on the watch → card returns to running; task ends → card shows the outcome and disappears after 5 minutes.
