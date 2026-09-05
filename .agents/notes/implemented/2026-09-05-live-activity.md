# Live Activity for agent runs

State: implemented. Builds on the Apple Watch companion note (`implemented/2026-09-05-apple-watch-companion.md`).

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
| run becomes `running` and is still alive after 10s | `start` (push-to-start) | 5 | 10s debounce in the ws node that owns the run, via `time.AfterFunc`; re-check state before publishing. The re-check accepts any non-terminal state, not just `running`: a run that turned `waiting_*` inside the window still deserves a card, opened at its actual phase. A lost timer on node restart means no card, which is acceptable. |
| `running` → `waiting_*` **with no card yet** | `start` at the waiting phase, with `alert` | 10 | No debounce. The debounce exists because most runs are a single tool-call round trip; a run that is waiting on its owner is precisely the kind that will not end in seconds, and an agent that asks for approval immediately is common — waiting out the window would leave the runs that most need a card without one. |
| `running` → `waiting_approval` / `waiting_question` | `update` with `alert` | 10 | Card switches to the waiting phase with `task_title`. |
| waiting → `running` (owner acted) | `update` | 5 | `chat_states` has no waiting→running write — being blocked is internal to the run — so this frame is emitted from `executeNotifyAction` after a successful approve / deny / reply, covering both owner-action entries (watch/app and the notification buttons). A resume that does start a new run instead arrives through the running path, which sends an `update` rather than a `start` because a card is already live. |
| `task_title` changes | `update` | 5 | Coalesce: at most one update per session per 5s. Renames happen in the api service, so the notifier lives in `internal/liveactivity` (outside `internal/ws`) and both services call it. |
| terminal (`completed`, `failed`, stop) | `end` with `dismissal-date = now + 5min` | 10 | Card shows the outcome, then leaves the lock screen. |
| stale-running settle (`SettleStaleRunningSessionAgentState`) | `end` | 5 | Cleans up cards whose run died silently. |

Gating: no new preference. A card starts only when the user still has the push channel on for at least one run-lifecycle notification (`approval_requested`, `agent_question`, `task_completed`, `task_failed`, `task_stopped_unexpected`), resolved through the same `ResolvePref` / `HasChannel(push)` machinery `dispatcher.dispatchPush` uses, and only to devices that reported a push-to-start token — which a device only does when the user has Live Activities enabled in iOS Settings. The gate is evaluated once, at start: once a card is on the lock screen its updates and its `end` are always sent, because a half-updated card or a zombie card stuck at "running" is worse than one extra push.

### Tokens

- **Catch-up on token registration.** Between publishing `start` and the device reporting its activity token there is a several-second window in which no update can be delivered — the backend has no token for that card yet — so a `waiting_*` transition landing in it is simply lost. `POST /v1/live_activities/token` therefore re-sends one frame matching the session's current `chat_states` state right after storing the token (`liveactivity.OnTokenRegistered`): an `update` while the run is live, an `end` when the run already finished inside the window (that `end` had no token to go to either, so the card would otherwise hang on the device forever). The catch-up frame never carries an alert: activity tokens are re-reported on rotation, so alerting here would buzz repeatedly, and a waiting run still has its own approval/question notification to sound.

- **Push-to-start token** (per device): reported through the existing `POST /v1/devices/bind` with a new optional field `live_activity_token`. Stored in a new nullable column `devices.live_activity_token` (VARCHAR 512). Migration adds the column; no other schema change.
- **Per-activity update token** (per device per activity): the iOS app reports it through a new authed endpoint `POST /v1/live_activities/token` with `{session_id, activity_id, token, device_id}`. Stored in Redis, key `im:la:tokens:<user_id>:<session_id>`, hash `device_id → {activity_id, token}`, TTL 24h. Activities are ephemeral; a table is unnecessary. `end` deletes the key after sending.
- Invalid tokens (`BadDeviceToken`, `Unregistered`, `ExpiredToken`, HTTP 410) clear the stored token — the `devices` column for a start token, the Redis hash field for an activity token — mirroring the existing invalid-device-token path. The device row itself is never deactivated: only the activity token is dead, the device's ordinary push channel is fine.
- A second Redis key, `im:la:sessions:<user_id>` (hash `session_id → started_at_ms`, TTL 24h), indexes which of a user's sessions currently have a card. It is what makes the 3-card cap enforceable without scanning keys, and what tells a transition whether it is opening a card or changing one. The ws side owns it; the push side owns the token key.

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

- New WidgetKit extension target `GrixActivity` (iOS) with `ActivityConfiguration(for: GrixRunAttributes.self)`: lock-screen banner, Dynamic Island compact / minimal / expanded, and `.supplementalActivityFamilies([.small])` so watchOS 11+ shows it in the Smart Stack. `widgetURL` carries the session id as `grix://session/<id>`; `AppDelegate.application(_:open:)` decodes it and calls the existing `notifyPushTap`, so the card reuses the notification-tap path instead of introducing a second navigation route. The `grix` scheme is registered in `Runner/Info.plist`.
- **The extension's deployment target is iOS 18.0, not 17.2.** `supplementalActivityFamilies` — the whole reason the watch Smart Stack needs no watch code — is iOS 18+, and a `WidgetConfiguration` modifier cannot be applied conditionally without registering two `ActivityConfiguration`s for one `ActivityAttributes` type, which is ambiguous at runtime. iOS 17.2–17.x therefore gets no card (the extension is simply not loaded) and keeps the existing notifications. The app itself still targets iOS 15; an extension with a higher minimum is allowed and installs normally.
- The card renders no status words of its own: the extension has none of the app's i18n. Phase is conveyed by SF Symbol and tint, and every string on the card (`title`, `detail`, the alert) is composed server-side in the recipient's language by `notification.LiveActivityPhaseCopy`.
- `ContentState.phase` is decoded as a `String`, not an enum. An unknown value from a newer backend degrades to the running style instead of failing to decode and freezing the card on its previous frame.
- `GrixRunAttributes` (shared Swift file between Runner and the extension): static `session_id`, `agent_id`, `agent_name`; `ContentState` = `phase`, `title`, `detail`, `updated_at_ms`. Phases are the `chat_states` names plus `stopped`.
- `Runner/Info.plist`: `NSSupportsLiveActivities = YES`.
- The four ways an owner can unblock a run all restore the card. `notify_callback.executeNotifyAction` covers the notification buttons and the watch; the in-app approval card and the in-app question answer arrive over WebSocket instead and are hooked at the same points that already mark the item resolved — `tryHandleExecApprovalCommand`, the hermes fallback rewrite, and `tryHandleClaudeQuestionCommand` (including its offline text-fallback delivery). Without these the card would sit at "waiting for you" until the run reached a terminal state.
- Native `LiveActivityBridge.swift` observes `Activity<GrixRunAttributes>.pushToStartTokenUpdates` and, for every activity (including ones already running via `activities` / `activityUpdates`), `pushTokenUpdates`, and hands tokens to Flutter over a MethodChannel `pub.dhf.grix/live_activity`. Tokens arriving before the Flutter side is up are buffered natively and drained on `drainPending`. Flutter sends the push-to-start token with the next device bind and posts activity tokens to `/v1/live_activities/token`. Flutter owns auth; native never calls the backend directly, and every report is fire-and-forget — never awaited inside the login flow, whose Dio interceptor would otherwise wait on itself.
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
- The bundle identifier `pub.dhf.grix.GrixActivity` must exist in the developer account before a signed build.
- Push volume rises by a few pushes per run; priority 5 for running updates keeps them coalesced by APNs.
- `chat_states` transitions become a second cross-service contract (ws → push) in addition to notifications; a renamed state must be reflected in the `phase` enum on iOS.
- iOS < 17.2 and devices without Live Activities enabled receive nothing and see no error.

## Verification

- `internal/push/live_activity_test.go`: `start` only targets devices with a push-to-start token; `update`/`end` use the Redis activity tokens; `end` deletes the token key; an invalid start token is cleared from the device row without retiring the device; an invalid activity token is removed from the hash; an unrecognised `cmd` is acked without error or sends.
- `internal/push/provider/apns_live_activity_test.go`: `SendLiveActivity` sets `apns-topic: <bundle>.push-type.liveactivity`, `apns-push-type: liveactivity` and the priority, and produces the documented `aps` shape for `start` (with `attributes-type` / `attributes`), `update` (alert, no attributes) and `end` (`dismissal-date`).
- `internal/liveactivity/notifier_test.go`: a run that reaches a terminal state inside the debounce window publishes nothing; a `waiting_*` transition publishes exactly one `update` carrying an alert; terminal publishes one `end` with `now + 5min` dismissal, clears the index, and a repeated terminal publishes nothing; a fourth card ends the oldest; resume and title changes only touch a live card, and three renames inside the window collapse to one update. For the gaps above: a waiting transition with no card starts one synchronously, at the waiting phase and with the alert, and a second transition then goes back to the update path; the debounced fallback opens a card at `waiting_question` without an alert; a card started with push disabled for every lifecycle event is not started at all; token registration re-sends exactly one alert-free frame matching the current state, ends a card whose run already finished, and ends rather than updates when the state is terminal while the index still holds the card.
- `internal/ws/agentapi/live_activity_resume_test.go`: an in-app approval and an in-app question answer each publish exactly one `running` update (verified against a fake JetStream, and confirmed to fail when the hooks are removed); a session with no live card publishes none.
- `internal/api/handler/live_activity_test.go`: `/v1/live_activities/token` stores under the caller's key with a bounded TTL, answers 403 — never 404 — for another user's or an unknown session, and triggers exactly one catch-up frame carrying the session's current phase.
- Migration `123_devices_live_activity_token.sql` applied, re-applied, dropped and re-applied on PostgreSQL 17; a NULL column reads back as an empty string through GORM.
- `xcodebuild` BUILD SUCCEEDED for Runner, GrixActivity and GrixWatch; `flutter build ios --release --no-codesign` succeeds and the product contains `PlugIns/GrixActivity.appex` (MinimumOSVersion 18.0); `flutter analyze` reports no issues.
- Not verified here, needs a device and the sandbox APNs environment: an iPhone (iOS 18+) with a paired watch (watchOS 11+) — run an agent task longer than 10s → card appears on the lock screen and in the Smart Stack; approval request → card switches to waiting with an alert; approve on the watch → card returns to running; task ends → card shows the outcome and disappears after 5 minutes.
