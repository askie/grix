# Direct reach drops the no-reply email channel

## Context

`POST /reach/direct` delivered through `in_app -> email -> sms`. The email leg can only
be sent from the shared no-reply DirectMail sender, which nobody monitors: a customer
replying to it is never read. Customer conversations are supposed to go out from the
monitored mailboxes (`kf@grix.im` / `gz@grix.im`), but any direct-reach call whose
`in_app` attempt was skipped silently fell through to that no-reply sender.

## Decision

The no-reply sender is reserved for verification codes. `/reach/direct` no longer has an
email channel at all:

- `directReachDefaultChannels` is `in_app -> sms`.
- `directReachChannelOrder` does not accept `email`; `channels: ["email"]` normalizes to
  an empty order, which `SendDirectUserReach` already rejects as a request error.
- The `case model.ReachChannelEmail` delivery branch and the now-unused
  `directReachEmailContent` envelope are gone. `SendDirectUserReachReq.EmailTemplateID`
  is removed with them.

The block lives in the channel normalizer rather than in caller conventions, so no caller
can re-enable email by passing a parameter.

## Alternatives

- Keep the branch behind a flag or an allowlist of callers: rejected for now because a
  reachable code path is exactly what caused the incident; an exception should be an
  explicit, reviewed change rather than a default.
- Fix only the default chain and keep `channels: ["email"]` working: rejected, it leaves
  the no-reply path one parameter away.

## Consequences

- `NotifyConnectorUpgrade` (`/connector/upgrade/notify`) passes no channels and therefore
  loses its email leg; it now goes `in_app -> sms`.
- The admin "inactive agent users" activation page (`admin/lib/modules/reach/`) posts
  `channels: ['email']` to `/reach/direct` and now gets a request error. That feature needs
  a follow-up decision: move it to the monitored mailbox flow or retire the email send.
- Verification-code mail is untouched: `sendEmailCodeInternal` builds its own DirectMail
  request and shares no code with the reach path.
- Other email senders are untouched: `SendReachEmailByTemplate` / `sendDirectReachEmail`
  stay in place for the connector-failure notify flow and marketing reach, which have
  their own channel selection.

## Verification

`backend/internal/api/service/reach_direct_service_test.go` covers the default chain never
touching email, explicit `channels: ["email"]` being rejected before any delivery, and the
`in_app` unavailable case landing on SMS. `TestDirectReachChannelOrder` pins the
normalizer's handling of `email`.
