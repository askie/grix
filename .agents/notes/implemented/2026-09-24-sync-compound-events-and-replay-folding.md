# Compound message sync events and replay folding

## Context

97% of `user_sync_events` rows are the `message.upsert`, `session.upsert` and
`session.unread_set` triple that one write derives from one message, and 16%
of message upserts repeat a message already upserted earlier in the same
replay page. Every shipped v2 client (v3.2.10+3018 onward) rejects a batch
unless each event cursor is exactly the previous one plus one and the last
equals `next_cursor`. A rejected batch is never acknowledged and is replayed
forever, so neither shared cursors nor cursor gaps may ever reach those
clients.

## Decision

- **Writers.** Every message-derived triple is built by
  `syncstream.MessageDeliveryEvents`. With `AIBOT_SYNC_COMPOUND_ENABLED=1` a
  delivery is one `message.upsert` row whose payload also embeds `session`
  (the `session.upsert` payload) and `unread` (the `session.unread_set`
  payload). The row reserves one cursor per part (`k = 1 + session + unread`):
  its `stream_cursor` is the last of them and the head advances by `k`. A
  delivery whose session event is itself a receipt (message edit) and every
  revoke stay classic rows.
- **Negotiation.** A client declares `compound_v1` in the existing
  `sync_resume.capabilities`; the server remembers it per resume.
- **Clients without `compound_v1`.** Each compound row expands into its classic
  rows at their reserved cursors. Cursor, kind, entity, version, receipt and
  payload fields are those of the classic write. These pages are never
  folded, and are cut at whole rows so they hold at most 100 events.
- **Clients with `compound_v1`.** They receive one event per row. An event
  carries `first_cursor` when it covers more than its own cursor: a compound
  span, or cursors folded away before it. The client checks
  `first_cursor == previous cursor + 1` and that the last cursor equals
  `next_cursor`.
- **Folding.** `AIBOT_SYNC_REPLAY_FOLD_ENABLED=1` folds each page of up to 100
  rows before it is sent, for `compound_v1` connections only.
  - `session` and `session_member` events keep the last per kind and entity.
  - `message.upsert` keeps the last per message. An upsert followed by a
    revoke of the same message keeps the revoke.
  - An embedded part is cleared when the same session changes later in the
    page. When a later event replaces a message but its embedded parts are
    still the latest, those parts are sent as classic events at their own
    cursors.
  - A superseded event stays when its `command_id` is not carried by any
    survivor, because it is the only receipt of that client command.
  - `session.remove` is not folded, because its reason decides whether
    messages are deleted.
  - `next_cursor` and `has_more` come from the unfolded page.
- Both switches default to off, and off means the previous behavior.

## Alternatives

- **Expanding into three events that share the row's cursor** (the first
  draft): rejected. Shipped clients fail on the second event and replay the
  page forever.
- **Folding for every connection**: rejected for the same reason, since
  folding leaves cursor gaps.
- **Relaxing the client check to strictly increasing cursors**: rejected, since
  `first_cursor` keeps the missing-cursor check for compound clients.
- **Building compound events at read time from classic rows**: rejected, since
  it saves no storage.
- **A new `caps` field**: rejected, since `capabilities` already exists on
  `sync_resume`.

## Consequences

- Stored `stream_cursor` values now have gaps.
  - Readers must query by cursor range, and derive a row's span from its
    payload with `fold.PartCount`.
  - `AppendTx` refuses an event whose `Span` disagrees with its payload.
- Clients without `compound_v1` keep today's stream. Only `compound_v1` clients
  receive fewer and smaller events.
- Events kept only as receipts are about 7% of the folded compound stream.
- In a folded page, a still-latest unread part can come before the surviving
  `session.upsert` of its session. If the client does not have that session
  yet, the unread update does nothing. The final unread snapshot of the
  `has_more=false` batch already reconciles every unread count, and that batch
  is the only one that publishes UI during catch-up.
- **Rollout.**
  1. Ship `ws` (the expanding reader) and every service that links a write
     point (`api`, `ws`, `llm`, `push`), with both switches off.
  2. Release the `compound_v1` client.
  3. Enable folding.
  4. Enable compound writes, and only once every `ws` replica runs the
     expanding reader. An older reader would send compound rows unexpanded.
- Either switch can be turned off at any time. Compound rows that were already
  written keep expanding.
- Once compound rows exist, `ws` must never roll back to a release without the
  expanding reader, even with the switch turned off. The rows stay in the log,
  and an older reader would serve them unexpanded to every client replaying
  across them.

## Verification

- `internal/syncstream/fold` tests cover the fold rules, receipts, the page
  bound, expansion, the event cap and empty pages.
- `internal/syncstream` tests cover:
  - classic output identical to the previous inline events;
  - compound rows expanding to the classic rows at the same cursors;
  - the span check in `AppendTx`.
- `internal/ws/handler` sync_v2 tests cover shaping by capability, folding
  only for `compound_v1`, and the classic page cap.
- `backend/cmd/syncbacktest` on the replica on 2026-09-24 (98455 rows):
  - baseline 98455, A 45905 (−53.4%), B 47681 (−51.6%), C 39701 (−59.7%);
  - classic expansion identical for every row;
  - no cursor-chain break;
  - no folded page whose final entity state or receipts differ from the
    unfolded page.
