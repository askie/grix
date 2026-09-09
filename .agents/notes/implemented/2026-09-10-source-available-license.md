# Relicense from Apache License 2.0 to a source-available license

## Context

The repository was licensed under Apache License 2.0. The maintainer decided
the project should remain visible and self-hostable for noncommercial use,
but should not be usable to run a competing commercial service, inside a
business, or as the basis for a mobile app published to app stores. All
history was authored by a single contributor, so relicensing does not require
third-party consent.

## Decision

Replace `LICENSE` with the Grix Source-Available License 1.0, a custom
noncommercial license, effective for the commit that introduces it and all
later versions. Four boundaries were fixed by the maintainer:

1. Commercial use is prohibited, and internal use within a business counts
   as commercial use — there is no "internal use" carve-out.
2. Publishing an iOS or Android build of the mobile client to any app
   distribution channel (app stores, public TestFlight, enterprise
   certificates) is prohibited, regardless of branding or modification.
   Building for one's own devices and installing by developer signing or
   sideloading for noncommercial use remains allowed.
3. Commercial or app-store distribution licensing inquiries go to
   `kf@grix.im`.
4. The new license takes effect the moment it merges to `main`; no grace
   period or dual-licensing window is defined.

A Chinese reference translation (`LICENSE.zh-CN.md`) is provided for
convenience; the English `LICENSE` text controls in case of conflict.
Bundled third-party components keep their own licenses as listed in
`THIRD_PARTY_NOTICES.md` and are unaffected by this change. No per-file
license headers were added.

## Alternatives

- **Keep Apache 2.0 and rely on a separate commercial terms document**:
  rejected because Apache 2.0 already grants unrestricted commercial use and
  sublicensing; a side document cannot retroactively restrict it.
- **Business Source License (BSL) with a future Apache re-licensing date**:
  rejected; the maintainer wants the noncommercial/app-store restrictions to
  be permanent, not time-limited.
- **AGPL**: rejected; AGPL still permits commercial use and internal
  business use as long as source is shared, which does not meet the
  "no commercial use at all" requirement.

## Consequences

- Copies already distributed under Apache License 2.0 remain under Apache
  License 2.0 for those copies; this is a one-way, non-revocable fact
  (Section 8 of the new license documents it, it does not create it).
  Forks or mirrors made before the relicensing commit can still be used
  under Apache 2.0 terms for that snapshot.
- `frontend/pubspec.yaml` now declares `license:
  LicenseRef-Grix-Source-Available-1.0` instead of `Apache-2.0`.
- `CONTRIBUTING.md` now states that submitting a contribution licenses it
  under the repository's current `LICENSE` and vests copyright in the
  Licensor.
- A repository-wide scan confirmed no direct dependency across
  `backend/go.mod`, `frontend/pubspec.yaml`, `admin/pubspec.yaml`, and
  `voicebridge/requirements.txt` uses a GPL/AGPL/LGPL/SSPL-family license,
  so none of them conflict with the new proprietary/source-available terms.

## Verification

- `git diff --stat` against the prior `main` shows only `LICENSE`,
  `LICENSE.zh-CN.md`, `README.md`, `README.cn.md`, `frontend/pubspec.yaml`,
  `CONTRIBUTING.md`, and this note.
- A repository grep for "open source" / "open-source" / "开源" / "Apache"
  (excluding `node_modules`, `build`, `.dart_tool`, `Pods`,
  `THIRD_PARTY_NOTICES.md`, and vendor directories) turned up only this
  repository's own README license notices (updated) and third-party license
  headers/notices (OpenHarmony scaffold files, `purify.min.js`,
  `WinSparkle`), which were intentionally left untouched.
