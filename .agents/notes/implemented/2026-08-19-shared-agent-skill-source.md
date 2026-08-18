# Shared Agent Skill Source

## Context

Grix is developed with multiple coding agents. Copying repository skills into tool-specific directories would allow the copies to drift, while not every tool discovers the same project path.

## Decision

Use `.agents/skills` as the only source of truth. Keep `.claude/skills` as a relative symlink to that directory because Claude Code discovers project skills there. Do not add tool-specific links when a tool already discovers `.agents/skills`.

## Alternatives

- Duplicate skills per tool: rejected because updates could diverge.
- Link every known tool directory: rejected because unsupported or duplicate discovery paths can create ambiguous skill loading.
- Keep only `.agents/skills`: rejected because Claude Code does not document that path for project skill discovery.

## Consequences

Tool-specific settings can still live beside the compatibility link and remain ignored. New compatibility links require evidence that the tool cannot consume `.agents/skills` directly.

Git clients must check out symbolic links as links; Windows environments with `core.symlinks=false` will not get Claude compatibility discovery. A client that scans both `.agents/skills` and `.claude/skills` may encounter two paths to the same skill, so validate client discovery after upgrades and remove the compatibility link if Claude adds native `.agents/skills` support.

## Verification

Validate each canonical skill through both `.agents/skills` and `.claude/skills`, and confirm Git records `.claude/skills` as a symbolic link.
