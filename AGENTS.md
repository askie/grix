# Grix Agent Guide

## Repository boundaries

- Keep changes inside the existing `backend`, `frontend`, `admin`, `voicebridge`, and `k8s` boundaries unless the task explicitly changes the architecture.
- Treat `README.md`, `CONTRIBUTING.md`, code, migrations, and tests as current facts. Decision notes preserve rationale; they are not a substitute for current documentation or tests.
- Follow `CONTRIBUTING.md`, including its requirement to keep commit authorship and contributor metadata limited to people who contributed.
- Do not commit credentials, private infrastructure details, generated build output, or local machine paths.
- Preserve unrelated work and use a dedicated git worktree for repository changes.

## Required workflows

- Use `$grix-code-review` when reviewing a diff, pull request, bug fix, feature, refactor, migration, protocol change, or security-sensitive change.
- Use `$grix-pre-push-checks` before a push or handoff to select the smallest sufficient validation set from the changed paths.
- Start with focused evidence. Broaden checks only when the change crosses modules or contracts.

## Shared agent configuration

- Treat `AGENTS.md` as the only source of truth for repository instructions. Keep `CLAUDE.md` as an import-only compatibility entry.
- Treat `.agents/skills` as the only source of truth for repository skills.
- Keep `.claude/skills` as a compatibility symlink to `.agents/skills`; edit the canonical files, not the compatibility path.
- Do not duplicate skills under `.cursor`, `.codex`, or other tool-specific directories when the tool already discovers `.agents/skills`.

## Cross-component contracts

When a change crosses a boundary, inspect every producer and consumer of the contract. Important boundaries include:

- REST and WebSocket payloads between Go services and Flutter clients.
- NATS subjects and messages between backend services and `voicebridge`.
- Agent adapters, session identity, ownership, permissions, and reconnect behavior.
- Database migrations and code that reads both old and new stored states.
- Kubernetes configuration, service health checks, and runtime defaults.

## Decision notes

Follow [`.agents/notes/README.md`](.agents/notes/README.md) for nontrivial decisions. Add or update a note only for durable rationale such as protocol or storage changes, authorization and security rules, cross-client behavior, or architecture tradeoffs. Do not create notes for routine fixes, tests, documentation edits, dependency bumps, or local refactors.
