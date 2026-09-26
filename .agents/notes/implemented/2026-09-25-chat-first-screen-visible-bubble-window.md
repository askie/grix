# 2026-09-25: Chat first-screen window measured in visible bubbles

## Context

ChatToolExecutionGroupProjector collapses consecutive same-sender
tool-execution cards into one group bubble. The first-screen auto-fill loop
and the resident window cap were both measured in raw rows (30-row initial
window + 4 pages x 40 rows auto-fill budget; 200-row resident cap). A session
whose tail is a long tool run (e.g. 190+ consecutive cards) collapsed to a
single bubble: auto-fill exhausted its raw-page budget mid-run with the
viewport still blank, and simply raising the budget would have made the
200-row raw cap trim the newest messages out of the window.

## Decision

- First-screen auto-fill terminates on visible outcome, not raw counts:
  viewport becomes scrollable, local history stops growing, the window already
  holds a resident-cap worth of *visible bubbles*, or the user scrolls.
  There is no raw page budget.
- The resident window cap counts visible bubbles using the exact chat-list
  visibility pipeline (`ChatMessageCardProjector.visibleUnitLengths`: all
  card projectors plus `isInternalDirectiveMessage`): collapsed
  tool-execution groups, folded exec/agent status cards and zero-height
  internal directives count once or not at all. Hidden rows merge into the
  preceding visible unit, so trim boundaries never split a collapsed group
  and the group count badge always reflects the rows actually held. A
  persistent `ChatMessageCardDecodeCache` keeps per-page accounting
  incremental (only new rows decode).
  A decode-free tool-run-only heuristic was rejected on review: it would
  count directives and folded status cards as visible and stop the fill
  early in approval/question-heavy sessions.
- Trim cuts are fate-aligned across non-adjacent folds: the projections
  expose `hiddenLeaderByIndex`, and the trim helpers extend (bottom) or pull
  back (top) the unit-boundary cut so a hidden follower and its leader are
  never separated. Keeping an approval card while dropping its resolved
  status would flip it back to a pending, re-tappable state; dropping the
  leader while keeping the follower would surface a standalone status card.
  Over-keeping raw rows at the boundary is the sanctioned tradeoff.
- The chat list delegate iterates `visibleMessageIndexes` from the snapshot,
  so collapsed rows and internal directives never materialize as zero-height
  children regardless of run length.

## Alternatives

- Raising the fixed budget/cap (4 pages, 200 rows): rejected — any longer
  tool run reproduces the bug, and a larger raw cap evicts newest messages.
- A LocalDb query skipping tool-card rows to jump straight to preceding body
  text: rejected — it splits the window around an unloaded middle range and
  breaks the older/newer cursor invariants.

## Consequences

- A pathological history consisting of one gigantic single-sender tool run is
  paged through row by row during auto-fill (memory grows with the run, all
  rows collapse into one bubble). Acceptable for realistic agent sessions.
- Expanding a very large group still renders all children eagerly; lazy
  detail rendering is a possible follow-up if this becomes measurable.

## Verification

- `test/modules/chat/chat_first_screen_tool_group_test.dart`: real
  LocalDb -> enterSession -> ChatView chain, 300 consecutive tool cards,
  no-gesture first screen, bottom pinning, expansion accuracy, gesture
  pagination.
- `test/data/providers/im_service_visible_window_trim_test.dart`:
  collapse-aware resident-cap trimming (tail run, mid run, plain-history
  parity, group-boundary alignment, synced-newest-replies-stay-visible,
  zero-height directives, exec approval/status pairs kept atomic,
  non-adjacent status card sharing its approval's fate across the cut).
- `test/modules/chat/message_cards/chat_message_card_projection_test.dart`:
  visible-unit accounting unit tests (directives, tool-run atomicity, exec
  pairing, all-hidden windows, prefix/suffix boundaries).
