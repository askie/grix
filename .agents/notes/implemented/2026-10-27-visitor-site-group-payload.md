# Visitor session site grouping payload

## Context

Widget visitor sessions already stored `widget_sessions.site_id`, but no client
payload exposed the owning site: session lists and the v2 `session.upsert`
projection only carried `is_visitor` (the list) or nothing at all (the v2
increment). Clients therefore could not group visitor conversations by the site
they came from, and a session created purely from the v2 increment stayed
misclassified as a normal chat.

`widget_sessions.last_page_url` cannot identify the site: it is overwritten on
every session reuse and follows the visitor across pages/paths. `Origin` is
validated on every widget init against `widget_sites.allowed_origins`, so its
host is the reliable "which site did this visitor enter through" signal.

## Decision

Store the first validated Origin host on the site and expose it additively:

- `widget_sites.first_origin` (`VARCHAR(255) NOT NULL DEFAULT ''`) holds the
  normalized host (lowercase, no scheme, no port). Widget init writes it with
  `UPDATE ... WHERE id = ? AND first_origin = ''`, so only the first visitor
  wins and concurrent inits cannot overwrite it. The migration backfills old
  rows from the site's earliest `widget_sessions.last_page_url` host, then from
  the first `allowed_origins` entry, otherwise leaves it empty.
- `SessionItem` / `ConversationItem` (REST session list, sync snapshot,
  conversations) and `FavoriteSessionItem` (favorites list) gain
  `visitor_site_id` (string) and `visitor_site_domain`. All are `omitempty`;
  the domain falls back to `site_name` while a site has no first origin.
  Every session-list-shaped response that a client renders must map them: the
  favorites list was the last one missing.
- Host normalization is one rule in two places: the migration's temporary
  `pg_temp.first_origin_host` and Go's `normalizeOriginHost`. Both drop the
  scheme, userinfo, port, path, query and fragment, lowercase, strip IPv6
  brackets, and treat `*` or anything unparseable as empty/invalid. Doing this
  with `split_part` alone produced `shop.example.com:8443?q=1` and
  `user:pass@example.com` in the backfill.
- Every v2 `session.upsert` projection (classic rows, `compound_v1` embedded
  session, revoke and membership producers) carries `is_visitor`,
  `visitor_site_id`, and `visitor_site_domain` for visitor sessions. The
  enrichment is a single joined lookup per session batch and is skipped when
  sync v2 is disabled.
- Non-visitor sessions serialize exactly as before: the visitor fields are
  `omitempty` and the payload type is unchanged.

## Alternatives

Deriving the site from `last_page_url` at read time was rejected: the column is
mutable per reuse and does not survive origin changes. Storing `origin` per
widget session was rejected because grouping is per site and the requirement is
the site's first entry domain, not the visitor's latest one. A separate
site-grouping endpoint would duplicate `is_visitor` handling and leave v2
incremental creation broken.

## Consequences

All API changes are additive; older clients ignore unknown JSON keys and keep
their current payload shape. `first_origin` is one-shot by design: a site that
later serves a different domain keeps the first one for grouping stability, and
rotating it requires an explicit data change. Grouping must key on
`visitor_site_id`, not the domain, because the domain can be missing or fall
back to the site name. Widget init pays one extra indexed `UPDATE`; v2 message
writes pay one indexed join per delivery batch when v2 is enabled.

`visitor_site_id` and `visitor_site_domain` are delivered to every human member
of a visitor session, including the visitor. The domain is public information
(widget sites only serve configured origins) and the id is opaque, so exposing
both is accepted; the site name and visitor PII are not added to these
payloads.

The paged `/conversations` endpoint still returns one visitor per site as
separate rows, so a client that groups by site only sees the rows it has
paged in; bulk pin/mute/delete on a site group therefore covers the loaded
subset on that path. That matches the pre-existing `visitor:group` behaviour
and is a paging/product limit, not a payload gap — the default local-first list
path reads every locally synced session.

## Verification

`backend/internal/api/service/visitor_site_group_test.go` covers list and
conversation payloads plus the non-visitor negative case;
`user_session_favorite_service_test.go` covers the favorites payload (visitor
fields present, plain favorites unchanged);
`widget_site_first_origin_test.go` covers one-shot Origin capture, config-only
requests not writing it, the v2 event emitted by widget init, and the
`normalizeOriginHost` edge cases;
`backend/internal/syncstream/session_visitor_meta_test.go` covers classic,
`compound_v1`, revoke and plain-session `session.upsert` payloads;
`backend/internal/store/migration_widget_site_first_origin_pg_test.go`
(`pgverify`) covers the PostgreSQL backfill order, idempotency, and host
normalization (port+query, userinfo, IPv6, uppercase, `*`, no scheme, fragment).

