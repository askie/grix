# Slash command descriptions: a second, dedicated i18n table

## Context

Every registered `agentslashcmd.SlashCommand.Description` was Chinese-only.
The toolbar's existing localization path (`agenttoolbar/i18n.LocalizeText`,
applied to every `Item`/`CommandItem` field in `core.localizeSnapshot`) only
maps zh -> en via a hand-written dictionary keyed by exact zh phrases, and
that dictionary never had entries for slash command descriptions. Worse,
the toolbar pipeline narrows the user's real language preference down to
zh/en before it reaches `localizeSnapshot` at all
(`resolver.LoadPreferredLanguage` = `tooli18n.NormalizeLanguage(userpref.Language(...))`),
so even adding entries to that dictionary could only ever produce English —
the app supports 11 languages (see `backend/internal/pkg/userpref/language.go`
`supportedLanguages` and the install guide's `localizedGuideText`), and the
other 9 always fell back to raw Chinese.

## Decision

Slash command descriptions get their own translation table,
`agentslashcmd.descriptionI18n` (`internal/agentslashcmd/i18n.go` +
`i18n_data_*.go`), instead of extending the toolbar's existing zh/en
dictionary:

- Keyed by the zh reference text (deduplicated once), not by
  `(client_type, command name)`. The same Chinese phrase repeats verbatim
  across many client types (e.g. "压缩当前会话上下文"), so a table keyed by
  text needs one entry per unique phrase (295 across the 27 registered
  client types as of round6) instead of one per command instance (326+).
  `DescriptionFor(zhText, lang)` resolves `lang -> en -> zh`, the same
  convention as the install guide's `localizedGuideText`/`pickGuideText`.
- Resolution happens in `core.localizeSnapshot`, scoped to
  `item.ItemID == "slash_commands"` only. The "skills" item also carries a
  `Commands` list (SKILL.md descriptions, arbitrary user/vendor text), and
  running those through a table keyed by exact Chinese phrases risks a false
  match rewriting someone's actual skill description.
- `BuildInput` carries the user's full language preference in a new field,
  `LanguageFull` (`userpref.Language`'s raw 11-way value), alongside the
  existing `Language` field (`tooli18n.NormalizeLanguage`'s zh/en-narrowed
  value) — every other toolbar text field keeps using `Language` unchanged.
  `localizeSnapshot` falls back to `languageFull = language` when
  `LanguageFull` is empty, so a future construction site that forgets to
  populate it degrades to the narrower zh/en behavior (still wrong for 9
  languages, but never silently blank) rather than resolving to raw zh
  unconditionally.

## Alternatives

- **Extend `agenttoolbar/i18n`'s existing zh/en dictionary to 11 languages
  and thread the full language code through the whole toolbar pipeline.**
  Rejected as out of scope: every other toolbar field (labels, tooltips,
  confirm text, ...) has the identical zh/en-only gap, and fixing it
  properly means auditing every construction site across ~20 agent
  packages, not just slash commands. Threading `LanguageFull` through as an
  additive field only for the one call site that needed it is a much
  smaller, self-contained change.
- **Add a `Descriptions map[string]string` field directly on
  `SlashCommand`, populated per command literal.** Rejected: with the same
  phrase repeating across client types, this duplicates every translation
  once per occurrence, and editing one occurrence without the others
  silently drifts the translations apart. A phrase-keyed table only has one
  copy to update.

## Consequences

- Adding a new client type's slash commands with a Chinese description that
  isn't already in `descriptionI18n` is not automatically translated —
  `DescriptionFor` falls back to the zh original for every language,
  matching the pre-round6 behavior rather than erroring. This is caught at
  test time (see Verification), not at runtime.
- Custom (user-authored) slash commands never pass through this table:
  `core.ApplyCustomSlashCommands` merges them into the snapshot *after*
  `localizeSnapshot` runs.
- Machine-translated, not human-reviewed (explicit product decision for
  round6 — see the round5/round6 entries in
  `2026-09-05-generic-acp-client-type.md`).

## Verification

- `internal/agentslashcmd`: `TestDescriptionFor_CoversAllRegisteredCommandsIn11Languages`
  walks every registered client_type's commands across all 11 languages,
  asserting non-empty, non-identical-to-zh-original (for non-zh languages),
  and that slash aliases/placeholders embedded in the text survive
  translation. `TestDescriptionI18n_NoOrphanedEntries` walks the table the
  other direction — every key must still back a live registered command,
  catching entries left behind when a command is renamed or removed (e.g.
  reasonix's `/restart`, dropped by round5's `13933609` but caught here
  after round6's table was written against the pre-round5 file).
- `internal/agenttoolbar/core`: `TestLocalizeSnapshotTranslatesToggleLockReason`
  and friends confirm the existing zh/en `tooli18n` path for every other
  toolbar field is unaffected.
