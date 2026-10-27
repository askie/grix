# Visitor flag persistence

## Context

The conversation list is built from the local session projection by default
(`USE_CONVERSATION_LIST_API=false`). A visitor session's identity was carried
only by `ImService._visitorSessionIds`, an in-memory set fed by session
snapshots and conversation summaries. `sessions` had no `is_visitor` column, so
after any app restart the row was rebuilt as a peerless private chat: it no
longer merged into `visitor:group`, lost the visitor label, and was treated as a
normal private thread by peer-identity backfill.

## Decision

Persist `is_visitor` as an integer column on the local `sessions` table
(schema v21, `INTEGER NOT NULL DEFAULT 0`) and treat the stored value as the
durable truth. The local projection reads `is_visitor` from the row and ORs it
with the in-memory set (which only covers flags that arrived this process but
have not been written yet). Server session snapshots, conversation summaries,
and `SessionModel.toJson()` all write the field. `VisitorInfo` (site, name,
email, last page) stays detail-derived and is not persisted: opening a visitor
chat fetches session detail because `is_visitor` is now true.

## Alternatives

Rehydrating the in-memory set from titles or peer metadata was rejected as
guesswork. Keeping the flag ephemeral and relying on the next server snapshot
was rejected because the local-first list renders before any snapshot returns
and a failed refresh leaves the wrong grouping permanently. A separate
visitor table was rejected as disproportionate for one boolean.

## Consequences

Old databases migrate with every existing row defaulting to `0`, so previously
known visitor sessions appear as normal sessions until the next successful
session snapshot or conversation summary refresh corrects and persists them. No
backend change is required: both endpoints already return `is_visitor`.

## Verification

`frontend/test/modules/home/controllers/conversations_visitor_flag_persist_test.dart`
covers the snapshot and summary write paths followed by a cold rebuild from the
database, plus a negative control for ordinary and group sessions.
`frontend/test/data/providers/local_db_migration_test.dart` asserts the v21
column and its `0` default on an upgraded legacy row.
