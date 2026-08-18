---
name: grix-pre-push-checks
description: Select and run the smallest sufficient validation set for Grix changes before push, review handoff, or completion. Use after changing backend Go code, Flutter frontend or admin code, Python voicebridge code, Kubernetes manifests, repository scripts, documentation, decision notes, or repository-local skills.
---

# Grix Pre-push Checks

Choose checks from the changed paths. Start with a focused check and broaden only when the change crosses packages, services, clients, or contracts.

## Determine the change set

1. Read the repository `AGENTS.md` and `CONTRIBUTING.md`.
2. Use the user-specified or pull request base when available; otherwise compare with the merge base of `origin/main` and include local staged, unstaged, and relevant untracked files.
3. Group changed files by subsystem and select checks below. A cross-component contract change requires checks for every affected producer and consumer.

## Select checks

- `backend/**`: From `backend`, run `go test` for the changed package first. For cross-package changes, run `AIBOT_TEST_NATS_URL=nats://127.0.0.1:1 go test ./...`; add `go vet ./...` and `go build ./...` when shared contracts or command entrypoints change.
- `frontend/**`: From `frontend`, run the closest `flutter test <test-file-or-directory>`. For shared state, navigation, protocol, or multi-feature changes, add `flutter analyze --no-fatal-infos` and `flutter test --timeout 90s`.
- `admin/**`: From `admin`, run the closest `flutter test <test-file-or-directory>`. Add `flutter analyze --no-fatal-infos` and the full `flutter test` when shared code or multiple features change.
- `voicebridge/**`: From `voicebridge`, run the changed module's `python -m unittest <test_module> -v`. For shared bridge behavior, run the non-network unit-test set documented in `voicebridge/README.md`.
- `k8s/**`: Render each affected base with `kubectl kustomize <directory>` when `kubectl` is available. Inspect the output for secrets, selectors, probes, ports, image references, and missing configuration. Never apply manifests as a validation step.
- `.agents/skills/**`: Run the skill creator's `quick_validate.py` against each changed skill directory and inspect its metadata for accurate triggering.
- Documentation and decision notes only: check links, paths, commands, and consistency with current code. Do not run application-wide tests solely for prose changes.
- Repository scripts or workflows: run the narrow script test or syntax check that exercises the changed path. Do not invoke publishing, deployment, release, upload, or credentialed jobs.

If a security-sensitive change is present and `gitleaks` is installed, run `gitleaks detect --source .`. Treat findings as blocking until they are removed or proven to match a committed narrow exception.

## Safety and completion

- Do not run production, destructive, paid, real-provider, release, deployment, or credentialed checks without explicit user authorization.
- Do not broaden to all repository tests when focused evidence is sufficient.
- Treat a failed required check as blocking. Investigate the failure or clearly report why it could not be resolved.
- Report commands run, pass/fail results, and relevant checks skipped with reasons. Do not claim validation that was not executed.
