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
durable truth. The same v21 migration also adds the site-grouping columns
`visitor_site_id` / `visitor_site_domain` (TEXT, nullable): v21 ships as one
release, so three columns and one cursor reset in one step beat an unshipped
v21 followed by a v22 that resets cursors a second time. The local projection
reads `is_visitor` and the site columns from the row and ORs the flag with the
in-memory set (which only covers flags that arrived this process but have not
been written yet). Server session snapshots, conversation summaries, and
`SessionModel.toJson()` all write the fields, and `LocalDbSyncRepository`'s
`session.upsert` projection applies `is_visitor` / `visitor_site_id` /
`visitor_site_domain` when the v2 event carries them (the backend enriches only
visitor sessions), so increments no longer leave a site group stale until the
next bootstrap. `VisitorInfo` (site, name, email, last page) stays
detail-derived and is not persisted: opening a visitor chat fetches session
detail because `is_visitor` is now true.

The migration resets **both** sync generations, because they live in different
stores:

- sync v2 cursors in `sync_state` (`committed_cursor`, `bootstrap_cursor`,
  `bootstrap_page_cursor`, `bootstrap_head_cursor`, `bootstrap_snapshot_cursor`,
  `bootstrap_in_progress`) plus `sync_bootstrap_sessions`;
- sync v1 uses `SharedPreferences.session_sync_cursor_<uid>`
  (`im_service_sync_state.dart`). SQLite alone cannot invalidate it, so the
  migration inserts a `pending_prefs_resets` row and `ImService` deletes the
  prefs key the first time it loads the cursor, then clears the marker. Deleting
  prefs first and the marker second means a crash in between repeats a harmless
  delete instead of leaving a stale cursor.

The first launch after the upgrade therefore runs one full refresh: sync v2
does one full bootstrap, sync v1 falls back to the window snapshot
(`fetchSessionSnapshotsResult`) instead of `session_sync` increments. In both
cases the server truth is written back to the visitor columns once, without
user action.

## Alternatives

Rehydrating the in-memory set from titles or peer metadata was rejected as
guesswork. Keeping the flag ephemeral and relying on the next server snapshot
was rejected because the local-first list renders before any snapshot returns
and a failed refresh leaves the wrong grouping permanently. A separate
visitor table was rejected as disproportionate for one boolean. A second
schema version (v22) for the site columns was rejected because v21 has not
shipped: it would double the cursor reset and make the upgrade path harder to
reason about.

The v1 prefs cursor was almost-cleared twice, so the mechanism is explicit: the
data layer never reads SharedPreferences during migration; it only records
intent, and the connection-owned sync path consumes it.

## Known limitation: `/conversations` paging

The default path (`USE_CONVERSATION_LIST_API=false`) builds the list from
`LocalDb.getSessions()`, which has no window or `LIMIT`, and the windowed
refresh only adds rows (out-of-window sessions are never deleted). One visitor
site's group therefore covers every locally synced session of that site, and
pin / mute / delete-whole-group act on all of them.

The `/conversations` API path is page-based: only the pages the user has
scrolled are loaded, so a site group's bulk actions act on the loaded subset
and threads beyond the loaded pages keep their old pin/mute state. This is the
same limitation `visitor:group` had before site grouping; it is documented in
`conversations_controller.dart` (site-group builder, pin, mute, delete) and is
intentionally not "fixed" here — the API path is not the default, and hiding
the remaining threads by paging is a product decision, not a bug in grouping.

## Privacy

`visitor_site_id` and `visitor_site_domain` are delivered to every human member
of a visitor session, including visitors. The domain is public information
(widget sites only serve allowed origins) and the id is opaque, so this is
accepted rather than masked; the site *name* and visitor PII are not added to
the list payloads.

## Verification

`frontend/test/modules/home/controllers/conversations_visitor_flag_persist_test.dart`
covers the snapshot and summary write paths followed by a cold rebuild from the
database, plus a negative control for ordinary and group sessions; its v1
upgrade case seeds a v20 database with a prefs cursor, asserts exactly one
window snapshot, corrected visitor/site columns, an untouched outbox row, the
server unread count, and no duplicated messages.
`frontend/test/data/providers/local_db_migration_test.dart` asserts the v21
columns, the single cursor reset, and the pending prefs reset marker on an
upgraded v20 database.

