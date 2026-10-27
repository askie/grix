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

Old databases migrate with every existing row defaulting to `0`. That default
is not authoritative, so the v21 migration also resets the sync cursors:
`sync_state.committed_cursor`, `bootstrap_cursor`, `bootstrap_page_cursor`,
`bootstrap_head_cursor`, `bootstrap_snapshot_cursor`, and
`bootstrap_in_progress`, and clears `sync_bootstrap_sessions`. The first launch
after the upgrade therefore runs one full sync v2 bootstrap (an old user would
otherwise only send `sync_resume` and receive incremental events, which carry
no `is_visitor`; the summary endpoint is off by default). Sync v1 users fall
back to the window snapshot because `committed_cursor` is zero. In both cases
the server truth is written back to `sessions.is_visitor`, so existing visitor
sessions are corrected once, without user action. No backend change is
required: session snapshots and conversation summaries already return
`is_visitor`.

The same limitation applies to any future session-level field that only exists
in the snapshot payload: sync v2 `session.upsert` projects only title, type,
peer id/type, updated time, and last message, so a new field (for example a
visitor `site_id`/`domain` for site grouping) is correct after bootstrap but
will not update through incremental events unless both the backend event
payload and `LocalDbSyncRepository._upsertSessionTx` are extended.

## Verification

`frontend/test/modules/home/controllers/conversations_visitor_flag_persist_test.dart`
covers the snapshot and summary write paths followed by a cold rebuild from the
database, plus a negative control for ordinary and group sessions.
`frontend/test/data/providers/local_db_migration_test.dart` asserts the v21
column and its `0` default on an upgraded legacy row.
