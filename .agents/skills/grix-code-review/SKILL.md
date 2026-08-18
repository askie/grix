---
name: grix-code-review
description: Audit Grix diffs and pull requests for correctness, regressions, security, lifecycle safety, and cross-component contract consistency. Use when reviewing backend, Flutter frontend or admin, voicebridge, database migration, Kubernetes, protocol, agent-adapter, bug-fix, feature, or refactor changes in the Grix repository.
---

# Grix Code Review

Review behavior, not formatting. Do not modify the change unless the user also asks for fixes.

## Establish the review scope

1. Read the repository `AGENTS.md`, `CONTRIBUTING.md`, and any narrower instructions.
2. Use the user-specified base when present. Otherwise use the pull request base, or the merge base with `origin/main` for local work. Include staged, unstaged, and relevant untracked files.
3. Classify changed files by subsystem and identify contracts that cross subsystem boundaries.
4. Read only the implementation paths, tests, migrations, and documentation needed to verify changed behavior.

## Audit the change

Trace each changed behavior from entry point to durable side effect and cleanup. Check:

- Correctness on success, error, retry, cancellation, timeout, and empty-state paths.
- Authentication, authorization, owner/session scope, input validation, secret handling, and sensitive logging.
- Concurrency, ordering, idempotency, reconnects, goroutine or subscription cleanup, Flutter disposal, and Python session shutdown.
- Backward compatibility for APIs, stored data, clients, agents, deployments, and rolling upgrades.
- Tests that exercise the actual entry path and at least one meaningful failure or negative-control path.

Apply subsystem-specific scrutiny only where relevant:

- `backend`: request boundaries, transactions, migrations, NATS subscriptions, resource ownership, and service-to-service failures.
- `frontend` and `admin`: state isolation, mounted/disposal safety, stale async results, platform differences, user-visible failure states, and payload compatibility.
- `voicebridge`: call-to-node ownership, start/mute/unmute/stop semantics, duplicate sessions, reconnects, and cleanup.
- `k8s`: credential safety, selectors, probes, configuration defaults, rollout compatibility, and rendered manifests.

For REST, WebSocket, NATS, database, agent-adapter, or voice changes, inspect every producer and consumer. Verify field meaning, optionality, defaults, ordering, error behavior, and version skew on both sides. Treat this as part of the review; do not create a separate protocol-review process.

Flag simplification only when evidence shows an unused production path, duplicated source of truth, or obsolete compatibility layer. Do not turn review findings into speculative redesign proposals.

## Verify and report

Use `$grix-pre-push-checks` when executable evidence is needed. Never run paid, production, destructive, or credentialed checks without explicit authorization.

Report findings first, ordered by impact. For each finding include the defect, precise file and line, user or operational impact, and evidence. Distinguish confirmed defects from questions. If there are no findings, say so and state the checks performed and any residual risk.
