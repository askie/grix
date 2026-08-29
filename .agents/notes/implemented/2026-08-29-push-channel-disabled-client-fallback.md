# Disabled push channels tell the client to fall back

## Context

`system_settings.push_channel` lets the owner switch an offline push channel off per
deployment. The delivery worker honored the switch by skipping the device, so a device
bound to a disabled channel silently received nothing: the worker never re-routes, and
the client had no way to learn that its channel was rejected. On the CN deployment every
`android_fcm` device was in exactly that state, because the cluster cannot reach Google.

The Android client already resolves a channel fallback chain natively
(vendor channel -> FCM -> JPush), but it only walks the chain when a channel fails to
produce a token locally.

## Decision

`POST /devices/bind` rejects a binding whose channel is disabled:

- HTTP `409` with business code `10012` and `data.disabled_platform`.
- The user's existing active binding for that device and that platform is deactivated,
  so no dead binding is left behind.
- Other platforms bound to the same `device_id` are untouched.

The client adds `disabled_platform` to an exclusion set, re-resolves the native chain with
`excludedPlatforms`, and registers the next channel that can produce a token. A channel
rejection does not consume the network retry budget; the exclusion set only grows, so the
loop terminates.

Clients cache their last successful binding. `push_binding_rev` was introduced and bumped
so an upgraded client re-registers once instead of trusting a cache entry that predates
this contract.

## Alternatives

- Re-route on the server: the server holds no token for the alternative channel; only the
  device can produce one.
- Leave the switch delivery-side only: it silences logs without delivering anything.
- Proxy Google egress from the CN cluster: moves an availability problem into the request
  path of every offline push and does not help devices whose vendor channel is preferable.

## Consequences

- Channels without an alternative (iOS APNs, Web Push) still have no fallback. Disabling
  those makes the affected devices stop binding; that is the intended meaning of the switch
  but it must not be used to work around a partially broken channel. Web Push in particular
  covers both Google and non-Google endpoints under a single flag.
- Clients older than this contract treat `409` as a bind failure: they do not cache and
  retry on the next launch, matching their previous behavior on a dead channel.
- Devices keep binding through the first channel that yields a token; the owner switch is
  the only signal that a channel should be skipped.

## Verification

- `backend/internal/api/service/device_service_test.go`:
  `TestDeviceBindRejectsDisabledChannel` covers reject, deactivation, and re-registration
  on the next channel.
- `frontend/android/app/src/test/kotlin/pub/dhf/grix/push/PushChannelResolverTest.kt`
  covers exclusion and the fully excluded chain.
