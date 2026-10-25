# Phone registration confirmation

## Context

Passwordless phone login created an account immediately after SMS verification.
A user intending to access an existing email account could therefore create and
occupy a second account without any warning. A public registration-status lookup
would enable phone-number enumeration, while existing clients must retain their
current automatic-registration behavior.

## Decision

Keep `POST /v1/auth/phone/login-code` as the only login/registration entry. An
opt-in request sends `supports_registration_confirmation=true`. Only after the
SMS code is verified, an unregistered phone receives
`requires_registration_confirmation=true` and a registration ticket, without a
user or auth token. Clients without the opt-in field keep automatic registration.

The ticket is a 256-bit random value stored in Redis for five minutes with the
normalized E.164 phone as its value. Confirmation consumes it atomically with
`GETDEL`, checks the phone, and creates the account without another SMS code.
Tickets are single-use; expiry, replay, and phone mismatch require a fresh code.
Updated Flutter clients show a confirmation dialog and resend the ticket only
after confirmation. Existing-account logins never show the dialog.

## Alternatives

A registration-status endpoint was rejected because it exposes account
existence before proof of phone ownership. Reusing the consumed SMS code would
require a second verification factor or relaxing replay protections. A signed
stateless token would still need server state to enforce single use, so it adds
signing and key-management complexity without removing Redis.

## Consequences

Backend instances must share the same Redis deployment. A delayed confirmation
can require a new SMS code, and a wrong-phone attempt consumes the ticket. The
API changes are additive, but backend must be deployed before clients that rely
on the challenge: older servers ignore the opt-in field and still auto-register.
Legacy clients remain compatible with the new server.

## Verification

Backend HTTP tests cover opt-in challenge without account creation, confirmed
registration, single use, expiry, phone binding, legacy auto-registration, and
existing-account login. Flutter tests cover challenge parsing, dialog cancel,
ticket confirmation, and no dialog for an existing account.
