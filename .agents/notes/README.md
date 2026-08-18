# Grix Decision Notes

Decision notes preserve rationale that future maintainers cannot reliably infer from the final code. They do not replace product documentation, API documentation, migrations, or tests.

## When to write a note

Write or update a note when a change makes a durable decision about:

- REST, WebSocket, NATS, agent, or voice protocol behavior.
- Storage schema, migration compatibility, or data ownership.
- Authentication, authorization, privacy, or another security boundary.
- Behavior that must remain consistent across clients or services.
- An architecture or reliability tradeoff likely to be reconsidered later.

Do not write a note for routine bug fixes, test additions, documentation-only changes, dependency updates, or refactors whose intent is clear from code and tests.

## Lifecycle and naming

Store notes at `.agents/notes/<state>/YYYY-MM-DD-short-title.md`. Create lifecycle directories only when needed.

- `proposed`: a decision not yet implemented.
- `implemented`: the current implementation embodies the decision.
- `rejected`: an alternative worth remembering because its rejection remains relevant.

Move the same note when its state changes; do not create lifecycle copies.

## Required content

Keep each note short and include:

1. `Context`: the constraint or problem.
2. `Decision`: the chosen behavior and its scope.
3. `Alternatives`: meaningful options considered and why they were not chosen.
4. `Consequences`: compatibility, operational, security, or maintenance effects.
5. `Verification`: tests or observable evidence that enforce the decision.

Update or remove a note when the decision no longer matches the implementation. Code and current documentation remain authoritative for present behavior.
