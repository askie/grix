# Apple Watch companion for Grix

State: implemented (Phase 1 + 1.5). Owner: architecture. Phase 2 remains out of scope; see the section below.

## Context

Grix users run long agent tasks and are frequently blocked on three owner actions: approve/deny a permission request, answer an agent question, or stop a run. Today these arrive as APNs pushes with action buttons (categories `APPROVAL_REQUEST` and `AGENT_QUESTION`, registered in `frontend/ios/Runner/AppDelegate.swift`) and a one-time `action_token` that the phone POSTs to `/v1/notify-callback` on the ws service.

Constraints that shape the design:

- Flutter has no watchOS target. Any watch UI is a native SwiftUI target inside `frontend/ios`.
- The only process that can approve/deny/stop/reply is the ws service, because `agentapi.Manager` owns agent connections (`backend/internal/ws/notify_callback.go`). The api service cannot execute owner actions.
- Action tokens are single-use, short-lived, and exist only inside the push payload. There is no server list of "pending owner actions"; the durable state is `chat_states` (`SessionAgentState`, one row per session/owner, states `running | waiting_approval | waiting_question | completed | failed | idle`, plus `task_title`, `agent_id`, `last_run_id`).
- Owner chat messages are sent over the user WebSocket; there is no REST "send message" endpoint.
- Refresh tokens rotate per use with family revocation on reuse (`internal/api/service/auth_service.go` `RefreshToken`). Access TTL 2h, refresh TTL 7d (`backend/config.yaml`). Two devices sharing one refresh token would log each other out.

## Decision

The watch is for clearing owner blockers without taking the phone out. Scope is deliberately narrow: approvals, questions, stop, a glance at agent state, and a dictated one-shot instruction. No chat history, markdown, files, groups, settings, billing, or login on the watch.

### Phase 0 — mirrored notifications (no code)

iOS notifications mirror to Apple Watch with the category's action buttons; text-input actions use dictation/scribble. The action response is delivered to the iPhone app, which POSTs `/v1/notify-callback` exactly as today. Phase 0 is validation only; see Verification.

Guardrails that must stay true for Phase 0 to keep working:

- The `NotificationService` extension must not drop `category`, `action_token`, or `mutable-content` from the payload.
- Action identifiers `approve | deny | stop | reply` and the two category identifiers are a cross-component contract between `internal/notification/dispatcher.go` (`categoryFor`) and `AppDelegate.swift`. Do not rename either side alone.

### Phase 1 — native watchOS app (minimum viable)

Features, in priority order:

1. **Inbox**: sessions whose chat state is `waiting_approval` or `waiting_question`, with agent name and `task_title`; actions approve / deny / stop / reply (dictation).
2. **Agents**: owner's agents with online flag and current chat state summary (running / waiting / completed / failed / idle), derived from `chat_states` joined to the agent list.
3. **Quick send**: pick an agent's latest session, dictate text, send as the owner. Show the last agent text reply, truncated, plain text.
4. **Complication / Smart Stack widget**: `pending` count and `running` count; tap opens Inbox.

Architecture:

```mermaid
flowchart LR
  P[iPhone Grix app] -- WatchConnectivity: access token + base URLs --> W[watchOS app]
  W -- HTTPS Bearer --> A[api service: read endpoints]
  W -- HTTPS Bearer --> S[ws service: owner action endpoint]
  S -- agentapi.Manager --> C[agent connector]
  A -. APNs .-> P -- mirror --> W
```

- **Auth**: superseded by Phase 1.5 — the watch now holds its own refresh family. The Phase 1 shape was: the phone pushes the current access token (never the refresh token) plus API/ws base URLs on every login and refresh, and the watch never calls `/v1/auth/refresh`. That made the watch dead 2h after the last time the phone was opened, which in practice reads as "the watch is usually broken". See **Phase 1.5 — the watch's own refresh family**.
- **Transport**: the watch calls the backend directly over HTTPS so it works on cellular/Wi-Fi without the phone. No WebSocket on the watch; the watch polls on foreground/refresh and relies on mirrored pushes for wake-ups.
- **New read endpoints (api service, existing `middleware.Auth()`)**:
  - `GET /v1/chat_states/list` — all `chat_states` rows for the caller as owner, each with `session_id, agent_id, agent_name, state, task_title, last_run_id, updated_at`, plus the agent's online flag. One call feeds Inbox, Agents, and the complication. Add `?state=waiting` to restrict to `waiting_approval|waiting_question`; any other value is `400`. Newest first, capped at 200 rows.
  - The response also carries `agent_provider_type`. Presence is reported by the connector only, so a remote-model agent (`provider_type=1`) is permanently `agent_online=false`; without the provider type the watch would render every such agent as offline and send users chasing a connection that does not exist. Rows whose agent has been deleted are omitted — no owner action on them can succeed.
- **New write endpoint (ws service, next to `/v1/notify-callback`)**:
  - `POST /v1/owner-action` with `Authorization: Bearer <access token>` and body `{session_id, action, text?}`, `action ∈ approve | deny | stop | reply | send`. The handler loads the caller's `chat_states` row for `session_id`, resolves the current `approval_command_id` / question target from the pending blocker record, and executes through the same `executeNotifyAction` paths as the notify callback. `send` dispatches an owner chat message into the session; it must persist and render on the phone exactly like a WebSocket send, so it calls `handler.HandleSendMsg` — the very function the user WebSocket routes `send_msg` to — through a detached `ConnInterface` that carries the caller's identity and captures the ack, not `DispatchOwnerCommandText` alone (that path is a command relay and does not persist).
  - Owner-only: reject when `chat_states.owner_id != caller`. Rate limit per user like the notify callback. Return `409` when the action no longer applies so the watch can drop a stale item.

  As implemented, staleness is per action rather than one blanket rule, because the states differ in what they permit:

  - `approve` / `deny` require `waiting_approval`; `reply` requires `waiting_question`. A mismatch is `409`.
  - `stop` requires only a non-terminal state — a run can be stopped while it is merely `running`.
  - `send` has no state requirement; quick send is not an inbox action.

  There is also a second staleness signal. `chat_states` stays in `waiting_approval` until the run itself ends, so a phone approval is invisible in that column. The handler therefore also checks the approval card index (`im:agent_api:approval_card:*`, deleted the moment the card is edited with its result) and returns `409` when the decision is already made. It fails open when Redis is unavailable: relaying a duplicate `/approve`, which the agent ignores, is cheaper than refusing a real one.

  **Pending blocker record.** Action tokens carry the target inside the push payload; the watch has no token, so the server needs its own record of what a blocked session is blocked *on*. `agentapi.SavePendingOwnerBlocker` writes `{kind, agent_id, approval_command_id | question_id + question_message_id, run_id}` to `im:agent_api:pending_owner_blocker:<owner>:<session>` (48h TTL, same lifetime as the approval card index) at the two points that already publish the notification and flip `chat_states` into a waiting phase. No schema change; a newer card overwrites the older record, which matches "a session is blocked on at most one thing".
- **Watch target**: `GrixWatch` (watchOS app, bundle id `pub.dhf.grix.watchkitapp`) and `GrixWatchWidget` (WidgetKit, `pub.dhf.grix.watchkitapp.widget`) in `frontend/ios/Runner.xcworkspace`, SwiftUI, Swift Concurrency, no third-party dependencies, `WATCHOS_DEPLOYMENT_TARGET = 10.0` (`containerBackground` and the Smart Stack both need watchOS 10). They build from `Flutter/Watch.xcconfig`, which includes only `Generated.xcconfig` — it supplies `FLUTTER_BUILD_NAME`/`NUMBER` so the watch version tracks the app, and deliberately omits the Pods xcconfig, whose iOS framework search paths do not apply to watchOS. `GrixWatch` is a target dependency of `Runner` and is copied in by an "Embed Watch Content" phase; the widget is embedded into the watch app the same way.
- **Complication data**: the watch app writes `pending` / `running` counts into the `group.pub.dhf.grix.watch` App Group after every refresh and calls `WidgetCenter.reloadAllTimelines()`. The extension only reads those two integers and never touches the network, so the access token stays in the watch app's own keychain and out of the shared container. The App Group must be registered for the two watch bundle ids before a signed device build or archive; simulator builds do not need it.
- The Flutter side calls one MethodChannel (`pub.dhf.grix/watch_session`) from `AuthService._persistTokens` — the single funnel for both login and refresh — and again on logout to clear the watch. Native forwards to `WCSession.updateApplicationContext`.
- **Release**: no release-script change was needed. `release-frontend.sh ios` runs `flutter build ipa` on the `Runner` scheme, and the target dependency plus embed phase carry both watch targets into the archive (`flutter build ios` prints "Watch companion app found"). App Store Connect needs watchOS screenshots on first submission, and the two new bundle ids plus the App Group need provisioning before a signed build.

### Phase 1.5 — the watch's own refresh family

Phase 1 gave the watch a copy of the phone's access token and nothing else, so it stopped working 2 hours after the phone was last opened. Phase 1.5 gives the watch a refresh family of its own.

- **New endpoint (api service, `middleware.Auth()`)**: `POST /v1/auth/watch/issue`. The caller authenticates with the *phone's* access token and gets back a fresh access + refresh pair in a brand-new family. The phone's own refresh token never leaves the phone. Rate limited per user (`auth-watch-issue`, 10 per minute): each call churns a refresh family.
- **No schema change.** The watch family is an ordinary row set in `auth_refresh_tokens`, and its `login_device_sessions` row carries `platform = "watch"` with a deterministic `device_id` of `watch:<user_id>`. `platform` is the *only* thing that distinguishes a watch family, so every rule that must cover the watch keys off it. The deterministic device id makes "one watch credential per user" a database constraint: the existing unique index on `(user_id, device_id) where revoked_at is null` cannot hold two.
- **Re-issue revokes first.** `IssueWatchTokens` revokes every live watch session of the user (and its family) inside the same transaction before minting the new one, then calls `security.MarkLoginSessionRevoked` after the commit so the superseded access token dies immediately rather than at its TTL.
- **The phone issues on login, and on demand** — `AuthService.ensureWatchCredentials()` is the single funnel; `_persistTokens(issueWatchCredentials: true)` from `_applyAuthPayload` covers login, register, QR exchange and account switch, and the phone's own token refresh still passes `false`. Phase 1.6 adds the on-demand entries; see it for why on-demand is not the periodic re-issue rejected in Alternatives.
- **The watch renews itself** through the same `POST /v1/auth/refresh` the phone uses; only the family differs, so the two rotate independently. It renews proactively within 5 minutes of expiry and reactively on a 401, with a single in-flight renew task — two concurrent renews with the same token would look like a replay and revoke the family. A 5xx is reported as a server error, never as "re-sync on your iPhone": only an actual 401 means the family is gone.
- **Revocation covers the watch by construction.** Password reset and any full logout already revoke *all* of a user's families and sessions. The one hole was a session-scoped phone logout, which by design touches only the caller's family; `LogoutWithToken` now revokes the user's watch families alongside it. Kicking one device from the device list still kicks exactly that device — the watch appears there as its own `platform = watch` entry and can be revoked on its own.
- **Residual gap** (closed in Phase 1.6): a watch left untouched for longer than the refresh TTL (7d) went stale and needed the phone to log in again.

### Phase 1.6 — closing the credential-sync gap

Issuing only on login left every already-logged-in user with a watch that never received a credential: nobody logs in a second time. Two separate defects, both user-visible as "打开 iPhone 上的 Grix 重新同步" forever.

- **The issue request was unauthenticated.** `_issueAndSyncWatchCredentials` posted to `/auth/watch/issue` on `AuthService`'s own `Dio`, which carries only the locale interceptor — the auth interceptor is what `attachAuthInterceptor` hands to *other* services. Everything authenticated in that file passes `Options(headers: {'Authorization': ...})` explicitly, and this one did not, so the `middleware.Auth()` route answered 401 and the failure was swallowed into a `debugPrint`. It now sends the granted token explicitly and logs http status plus `code`/`msg`; it does not call `ensureTokenFresh()`, because the caller has just minted that token and waiting on the login flow from inside it would deadlock.
- **On-demand issue, driven by the native side.** The phone re-checks on cold start, on `didBecomeActive`, and on `sessionWatchStateDidChange` (the user just installed the watch app). The check is local and cheap: `isPaired && isWatchAppInstalled` and the *last application context the phone itself pushed* carries no access token. Only then does `WatchSessionBridge` call back into Dart over the existing `pub.dhf.grix/watch_session` channel. This is not the rejected periodic re-issue — it fires only when the phone can see that the watch was never given anything.
- **The watch can ask.** With no credential, or when a renew fails as `unauthorized`/`notConfigured`, the watch sends `{"request": "request_credentials"}` — `sendMessage` when reachable, `transferUserInfo` queued otherwise. A logged-out phone answers with an empty payload so the watch drops a stale token. Both sides throttle to one request per 60s, because every issue revokes the previous watch family: an unthrottled retry loop would invalidate the credential it had just been given, and the server bucket is 10/minute.
- **Recovery is automatic.** `WatchStore` observes `WatchCredentialProvider.$credentials` and reloads when a usable credential lands, so `ResyncNotice` clears without restarting the watch app. A refresh already in flight when the new credential arrives is generation-checked: its failure no longer forces the UI back to "re-sync".

### Phase 1.5 — reply readback

`AVSpeechSynthesizer` reads the last plain-text agent reply out loud, and only when the user taps 朗读: the watch is on a wrist, and auto-playback would decide for the user in a meeting. The voice follows `AVSpeechSynthesisVoice.currentLanguageCode()` with no separate setting. watchOS needs the audio session put into `.playback` / `.spokenAudio` and activated first, or the synthesizer fails silently; it is deactivated again when the utterance finishes. After a quick send the composer stays open and polls for a new reply (12 rounds, 5s apart, cancelled when the view goes away) so the reply can be read without opening a chat. This is the non-real-time half of the Phase 2 walkie-talkie idea; LiveKit still has no watchOS SDK.

### Phase 1.5 — owner-action rate buckets

`send` moved to its own bucket (`owner-action-send`, 30 per 5 minutes). `approve | deny | stop | reply` stay on the notify callback's budget (10 per 5 minutes). They are different activities: clearing a blocker answers a notification and is naturally rare, while dictating messages is ordinary chatting. Sharing one bucket meant a talkative minute on the watch could lock the user out of approving anything — exactly the thing the watch exists to do.

### Phase 2 — deferred, not in scope

- Live Activity for "agent running" (ActivityKit push tokens on the backend). Also appears in watchOS 11+ Smart Stack for free.
- Walkie-talkie voice: dictate → send → read the reply with `AVSpeechSynthesizer`. LiveKit has no watchOS SDK, so no real-time call.

## Alternatives

- **Relay everything through the iPhone over WatchConnectivity** and reuse the phone's WebSocket. Rejected: the watch becomes useless the moment the phone is out of Bluetooth range, which is the main reason to look at a watch.
- **Share the refresh token with the watch**. Rejected: rotation with family revocation means one device's refresh invalidates the other's session. The follow-up named here — a dedicated watch refresh family — is what Phase 1.5 implemented.
- **Re-issue the watch credential on every phone token refresh** (instead of only on login). Rejected in Phase 1.5: issuing revokes the previous watch family, so a phone that refreshes while the watch is out of Bluetooth range would kill a watch credential that was working perfectly. The phone cannot tell a healthy watch from a stale one, so any phone-driven periodic re-issue is a coin flip against the one scenario the watch exists for.
- **Reuse `/v1/notify-callback` from the watch** by minting a token on demand. Rejected: tokens are single-use and bound to one notification event; the Inbox needs to act on the current state, not on a past push.
- **WebSocket on the watch**. Rejected for Phase 1: watchOS background networking is restricted, and mirrored pushes plus on-foreground polling are enough for an inbox.

## Consequences

- One new read endpoint on the api service and one authenticated write endpoint on the ws service. No schema change; `chat_states` already carries what the watch needs.
- The ws service gains a second entry point that executes owner actions; it must enforce ownership and rate limits as strictly as the notify callback.
- The iOS build grows two native targets; CI and the release script must build them, and Sentry/dSYM handling should include them.
- Cross-client contract: `chat_states` state names are now consumed by three clients (Flutter, connector MCP `grix_chat_state_query`, watch). Renaming a state requires touching all three.
- `platform = "watch"` on `login_device_sessions` is now load-bearing for authorization, not just a display label. Anything that enumerates or revokes sessions must keep treating it as a real session, and the watch shows up in the user's device list.
- The watch credential payload over WatchConnectivity now carries a refresh token. `WatchSessionBridge` / `WatchCredentialSync` / `WatchCredentialProvider` share those key names as a contract; the watch keychain record tolerates a Phase 1 archive with no refresh token.
- WatchConnectivity now carries a second message in the watch → phone direction: `{"request": "request_credentials"}`, delivered by `sendMessage` or `transferUserInfo`. An older phone build ignores it silently, and an older watch build simply never sends it, so either half can ship first.
- `pub.dhf.grix/watch_session` is now bidirectional: native calls Dart's `ensureCredentials`, and Dart calls native's `checkCredentialsState` once `AuthService.init()` has registered its handler (the native cold-start check can otherwise land before the Dart side exists). Both directions tolerate a missing peer method.
- `notification.BuildClaims` and the `ownerActionExecutor` interface around `executeNotifyAction` are exported/extracted so a test can prove both entry points emit the same agent command. Keep them that way: the parity test is the only thing standing between the two paths drifting apart.
- The ws service now authenticates access tokens itself (`authenticateBearer` in `internal/ws/owner_action.go`) because it is plain `net/http` and cannot use the gin `middleware.Auth()`. The two must stay in step — revocation, password-change invalidation and the disabled-user check are duplicated there deliberately.

## Verification

Phase 0 (manual, needs a paired Apple Watch, iPhone locked):

1. An agent requests approval → the watch shows the notification with Approve / Deny / Stop task.
2. Tap Approve → `/v1/notify-callback` logs `action=approve` and the agent continues.
3. An agent asks a question → tap Reply, dictate → the agent receives the text.
4. `.authenticationRequired` actions execute while the watch is unlocked on the wrist.

Phase 1, automated (all green):

- `backend/internal/ws/owner_action_test.go` — `owner-action` rejects an unauthenticated caller (401), a non-owner (403, never 404), an unknown action and an empty session id (400), a terminal or mismatched waiting state (409), an approval already settled on the phone (409), and enforces the per-user rate limit while leaving a second owner's budget untouched.
- `backend/internal/ws/owner_action_test.go` — parity: for one pending approval, the claims rebuilt from `chat_states` + blocker match the claims signed into the push action token, and running both through `executeNotifyAction` emits the identical `/approve <id> allow` command to the same agent.
- `backend/internal/api/handler/chat_state_test.go` — `chat_states/list` returns only the caller's rows, honors `state=waiting`, rejects any other filter value, orders newest first, omits deleted agents, and reports `agent_provider_type`.
- `backend/internal/api/service/auth_watch_token_service_test.go` — the issued watch family is distinct from the phone's and does not reuse its device id; the phone's family survives both the issue and a watch rotation; a re-issue revokes the previous watch refresh token and access token and leaves exactly one live watch session; a session-scoped phone logout kills the watch family; a password reset kills it too.
- `backend/internal/api/router_test.go` — `POST /v1/auth/watch/issue` is 401 with no bearer and with a bogus one.
- `frontend/test/data/providers/auth_service_watch_credentials_test.dart` — the issue request carries `Bearer <granted token>`; a fresh login issues even though `_isLoggedIn` has not flipped yet at that point in `_applyAuthPayload`; a 401 never pushes anything to the watch; a logged-out phone does not issue but answers a watch request with an empty payload; a native-triggered ensure issues once and is then throttled; a credential minted for a session that ended in the meantime is dropped.
- `backend/internal/ws/owner_action_test.go` — the `send` bucket and the blocker bucket are independent in both directions.
- Watch build: `xcodebuild -target GrixWatch -sdk watchsimulator` builds both watch targets; `flutter build ios --release` and `xcodebuild archive` on the `Runner` scheme both produce `Grix.app/Watch/GrixWatch.app/PlugIns/GrixWatchWidget.appex`.

Phase 1, still manual (needs a paired Apple Watch and a signed build):

- With Bluetooth off on the phone, the Inbox loads and an approval succeeds; after the phone resolves an item, the watch drops it on the next refresh; complication counts equal Inbox length.
- More than 2 hours after the phone was last opened, and with the phone out of range, the watch still loads the Inbox (it renewed on its own). Logging out on the phone makes the watch show the re-sync notice on its next call.
- Tapping 朗读 speaks the reply out loud through the watch speaker, and a second tap stops it.
- `release-frontend.sh ios` uploads the watch targets with the iOS app under real signing.
