# iOS offline notification delivery

## Context

Agent approval and question notifications never reached an iOS device in production even though the server-side chain was healthy: the device was active, notification preferences were on, dispatch receipts existed, and APNs answered 200 for every send. Four independent defects cancelled the delivery out on the client and in the payload.

## Decision

1. **Foreground alerting is event-scoped, not session-scoped.** `AppDelegate.userNotificationCenter(_:willPresent:)` still silences a banner when the notification belongs to the session the user is already viewing, but `approval_requested` and `agent_question` are exempt and always present `[.banner, .sound, .badge]`. The dispatcher force-pushes these events precisely because they need an answer, and the user is normally sitting in that session when the agent asks — the same-session silencing cancelled the force-push out entirely.
2. **The app claims the Time Sensitive entitlement.** `com.apple.developer.usernotifications.time-sensitive` is declared in `Runner/Runner.entitlements`, which all three Runner build configurations share. Without it iOS silently downgrades the `interruption-level: time-sensitive` the APNs provider already sets, so the notification cannot break through Focus. The notification service extension does not need the entitlement.
3. **Unreachable Web Push endpoints are retired by consecutive-failure count, and iOS is delivered first.** A Web Push endpoint that times out never returns an invalid-token status, so `isTokenInvalid` can never retire it: it stays active forever, fails every retry, and keeps the whole task in retryable-failure state so NATS redelivers it indefinitely. Five consecutive transport failures (counted in Redis under `push:webpush:fail:<device_id>`, 7-day TTL, cleared by any success) now deactivate the device and remove it from `im:user:devices:<uid>`, with a `device_web_push_unreachable` audit entry. Devices are also ordered iOS → other → `web_push` before delivery, and the Web Push send timeout dropped from 8s to 5s, so APNs is never queued behind a dead browser endpoint.
4. **Image messages carry a signed image URL.** For `msg_type=2` the worker resolves the image from `extra.media_url`, then the first image entry of `extra.attachments[]`, then the inline `![image](<url>)` in the content. Media objects live in a private bucket, so the URL is signed with the same signer the API uses and is only sent when the result is an `https://` URL. The APNs payload carries it as the top-level custom field `image_url`; the notification service extension downloads it as the attachment (identifier `image`) instead of the sender avatar, and drops the attachment above 10MB. The alert body stays `[图片]`.

## Alternatives

- *Silence approvals by category instead of event key*: the APNs `category` drives the action buttons and is not carried in a form the foreground handler can rely on for every producer; `event_key` is already written as a top-level custom field by the APNs provider and is the value the backend reasons about.
- *Retire the Web Push endpoint on the first transport failure*: a single timeout is routinely a transient network fault, and retiring a live browser subscription silently loses a delivery channel the user cannot re-register without noticing.
- *Send the raw stored media URL*: media lives in a private bucket, so the extension — which runs without the app's credentials — could not fetch it.
- *Sign the image URL at each push-task producer*: four separate services publish `im.push.offline.*`; signing once in the worker keeps one source of truth.

## Consequences

- `provider.PushPayload` gains `ImageURL`; the field is additive and only the APNs provider reads it, so FCM, JPush, vendor and Web Push payloads are unchanged. Older iOS builds ignore the unknown `image_url` custom field.
- The push service now calls `apiservice.InitOSS()`. It already mounts the same backend config map and env secret as the API, so no deployment change is required; if OSS is unavailable the worker logs a warning and image pushes degrade to text-only.
- Signing runs a `StatObject` check per media object, memoized for 5 minutes by the existing signing cache.
- The Apple Developer App ID must have the Time Sensitive Notifications capability enabled before a build using this entitlement can be signed.

## Verification

- `backend/internal/push/worker_test.go` covers deactivation after five consecutive Web Push timeouts, counter reset after a success (non-consecutive failures keep the device active), the iOS → FCM → web_push delivery order, and an `msg_type=2` push carrying the signed `image_url` with a `[图片]` body.
- `resolvePushImageURL` is unit-tested for extra precedence, attachment filtering, markdown fallback, and the https-only rule.
- `flutter build ios --no-codesign` builds the Runner and the notification service extension.
