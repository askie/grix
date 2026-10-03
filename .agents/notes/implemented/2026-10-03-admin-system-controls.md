# Admin system controls and registration admission

## Context

Admin needs a small extensible system parameter page. Registration already uses
`auth_register`, but a one-minute per-process feature cache and stale-on-error
fallback cannot enforce closure across instances.

## Decision

Expose a fixed ordered parameter directory under settings permission, with
per-key typed readers, validators and writers. `registration_enabled` maps only
to the existing `feature_gates.auth_register`. Missing or non-enabled status is
closed; no migration changes stored values. Writes atomically upsert and lock the
canonical row and audit the actor, key, and before/after values in one transaction.
Future JSON parameters can reuse `SystemSetting` without a remote config engine.

All new account and explicit registration-code paths call an authoritative direct
DB reader. Errors deny admission. Other feature caches remain unchanged. Auth
methods expose only registration capability and regional phone capabilities;
existing login, verified OAuth email binding and password reset stay independent.

## Alternatives

A second registration setting risks diverging from legacy Feature Gates. Local
cache invalidation leaves other instances stale; distributed invalidation adds
failure paths for a low-volume admission check. A distributed lock across Admin
writes and all registration transactions costs more than the required boundary.

## Consequences

Each admission check adds one DB read. After close commits, subsequent checks
reject across updated instances; a request that already passed may finish.
Rolling deployments need all backend instances updated for that guarantee.
Older clients ignore the additive field, and new clients tolerate old servers
omitting it. Admin retains separate confirmed/draft state and serializes reads
and writes. Unknown types remain read-only until an editor is explicitly added.

## Verification

API tests cover strict values, authentication/permissions, canonical storage,
legacy writes, audit and rollback. Actual service tests cover four account
creation paths and existing accounts, warming enabled cache before an independent
connection closes the gate or a query error occurs. Flutter tests cover metadata,
boolean payloads, failure/retry/draft state, regional response ordering, direct
registration denial, and phone/wide Admin layouts. See
[system controls](../../../admin/docs/system-controls.md) for regression commands
and the PostgreSQL/provider verification limits.
