# Group chat title authority is group_name

## Context

Group chats stored display titles in two places: `sessions.group_name` (create / convert) and `session_members.custom_title` (via `/sessions/rename`). Session list preferred `custom_title`, while local sync `session.upsert` wrote `group_name` into the client title. Clients therefore flickered between names (for example「你好」vs `Dhfphp`) without anyone opening group settings.

## Decision

- Group chats use `group_name` as the only display title source.
- `/sessions/rename` on a group updates `group_name`, clears all human members' `custom_title`, and rejects an empty name.
- Private chats keep writing `custom_title` (empty clear still allowed).
- `resolveSessionTitle`, agent-state title denormalisation, and webhook session titles follow the same split.
- Migration `130_heal_group_title_from_custom_title.sql` copies non-empty group `custom_title` into `group_name`, then clears those `custom_title` values so historical renames are not lost.

## Alternatives

- Keep dual fields and teach every client to prefer one consistently: still drifts when rename and sync disagree.
- Add a separate group-rename API: unnecessary; the existing rename endpoint can branch on `session_type`.

## Consequences

- Group rename no longer creates a personal-looking alias layer; all members see the same `group_name`.
- Empty rename is valid only for private chats.
- Clients that still show a cached `custom_title` converge after list refresh / sync once migration has run.

## Verification

- `TestSessionRenameGroupWritesGroupName`
- Session list test `group title ignores stale custom_title`
- Existing private `TestSessionRename` cases remain green
