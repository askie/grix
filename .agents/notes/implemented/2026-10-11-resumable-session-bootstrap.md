# Resumable session bootstrap

## Context

New devices previously fetched every session through one mutable-order
`limit=10000` request. Accounts above that bound could never finish, repeated
failures could reconnect forever, and offset paging by pin/activity order would
skip or duplicate rows while messages updated `last_active_at`.

## Decision

The opt-in `sync_head=1&bootstrap_keyset=1` path pages at most 200 rows by
`session_members.bootstrap_id`, a PostgreSQL sequence-backed, immutable column.
The first page captures `sync_head_cursor`; later pages send that fixed head and
an exclusive `bootstrap_cursor`. The client persists the page cursor, captured
head, first-page snapshot cursor, and all enumerated session IDs in SQLite after
applying each page. It resumes from that durable cursor after reconnect or cold
start, reconciles deleted local sessions only after the final page, then starts
normal sync-v2 replay from the captured head.

Legacy `sync_head=1` list behavior remains available for rolling compatibility.
Bootstrap failures use bounded retry/backoff and open a visible circuit after
five attempts. Exact duplicate bootstrap-page requests from one login device
are rejected for 60 seconds with `429 Retry-After`.

## Alternatives

- Smaller offset pages using the normal list order were rejected because pin
  and activity changes move rows across offsets.
- `session_id` keysets were rejected because IDs are immutable but not
  monotonic; a newly created session can sort before the saved cursor.
- Restarting at page one after interruption was rejected because it repeats the
  expensive transfer and defeats resumability.

## Consequences

Migration `129_session_member_bootstrap_id.sql` backfills existing memberships
and indexes `(member_type, member_id, bootstrap_id)`. The client schema is v20.
New clients require that migration before using the opt-in path; old clients
continue to use the legacy endpoint. Deploy the migration and compatible
backend before releasing the new client; an older backend cannot produce a
continuation cursor for accounts above its response limit. A post-head
membership can appear in the remaining pages and again in sync replay, so
projection application must remain idempotent.

## Verification

The service test enumerates more than 10,000 memberships while activity and pin
fields change, checks no duplicates or omissions, and verifies a concurrently
inserted membership appends after the cursor. Handler, client API, SQLite
migration/progress, reconnect-resume, final reconciliation, retry-circuit, and
duplicate-rate-limit tests cover the cross-component contract.
