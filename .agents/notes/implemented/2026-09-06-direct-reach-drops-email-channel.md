# Customer reach drops the no-reply email channel

## Context

`POST /reach/direct` delivered through `in_app -> email -> sms`, and the admin
"connector upgrade failure" notifier (`POST /connector/reports/notify`) defaulted to
email. Both legs can only be sent from the shared no-reply DirectMail sender, which
nobody monitors: a customer replying to it is never read. Customer conversations are
supposed to go out from monitored mailboxes, but any direct-reach call whose `in_app`
attempt was skipped silently fell through to that no-reply sender.

## Decision

The no-reply sender is reserved for verification codes. Every customer-facing reach path
that fell back to it loses the channel outright, in the normalizer rather than in caller
conventions, so no caller can re-enable it by passing a parameter.

- `/reach/direct`: `directReachDefaultChannels` is `in_app -> sms`;
  `directReachChannelOrder` does not accept `email`, so `channels: ["email"]` normalizes
  to an empty order, which `SendDirectUserReach` already rejects as a request error. The
  `case model.ReachChannelEmail` delivery branch, the `directReachEmailContent` envelope
  and `SendDirectUserReachReq.EmailTemplateID` are gone.
- `/connector/reports/notify`: `ConnectorNotifyChannelEmail` is gone, `auto` resolves to
  SMS only, and an omitted `channel` is now a request error instead of defaulting to
  email — SMS is a dormant capability (no filed template), so a silent default would risk
  an accidental SMS blast. `PreviewConnectorNotify` no longer renders an email preview.
- `SendReachEmailByTemplate` is deleted: it was the last no-reply template sender and had
  no caller left. `sendDirectReachEmail` stays as a test seam only, so a re-added email
  call is caught by the existing `t.Fatal` stubs.
- Admin: the "inactive agent users" page loses its send path (composer, preview, results)
  and shows a disabled button; the connector problem-users page offers only `auto`/`sms`.

## Alternatives

- Keep the branches behind a flag or a caller allowlist: rejected, a reachable code path
  is exactly what caused the incident; an exception should be an explicit, reviewed change.
- Fix only the default chain and keep `channels: ["email"]` working: rejected, it leaves
  the no-reply path one parameter away.

## Consequences

- `NotifyConnectorUpgrade` (`/connector/upgrade/notify`) passes no channels and therefore
  goes `in_app -> sms`.
- The connector failure notifier has no working channel until an SMS template is filed;
  that is intended — its only working channel was the banned one.
- Verification-code mail is untouched: `sendEmailCodeInternal` builds its own DirectMail
  request and shares no code with the reach path.
- Still able to send from the no-reply sender: the marketing/announcement reach consumer
  (`deliverReachEmail` in `reach_consumer.go`, `reach_marketing_service.go`), which is a
  bulk opt-in flow with its own channel selection and unsubscribe handling. Left alone
  deliberately; retiring it is a separate product decision.

## Verification

`backend/internal/api/service/reach_direct_service_test.go` covers the default chain never
touching email, explicit `channels: ["email"]` being rejected before any delivery, and the
`in_app` unavailable case landing on SMS. `TestDirectReachChannelOrder` pins the
normalizer. `connector_upgrade_failure_notify_service_test.go` covers the rejected email
channel, the rejected empty channel, `auto` using SMS only, and asserts the email stub
records nothing.
