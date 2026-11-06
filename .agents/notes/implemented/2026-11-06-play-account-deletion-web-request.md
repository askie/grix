# Play account deletion web request

## Context

Google Play rejected the listing for User Data → Account Deletion Requirement because the Data safety / account deletion URL was considered invalid. Play requires a public web resource where users can request account and associated data deletion without reinstalling or opening the app. A page that only documents the in-app deletion path does not satisfy that check.

## Decision

Serve `https://gb.grix.im/legal/account-deletion` as the declared Play URL. The page’s primary pathway is an email deletion request to `support@grix.im`; in-app deletion remains an optional secondary path. The page names the Play listings (`Grix Ai` / `虾塘`) and package `pub.dhf.grix`.

## Alternatives

- Web login + self-serve delete UI: better UX, but larger scope than needed for compliance.
- Only point users back to the app: rejected by Play policy and review.
- Google Form / third-party form host: workable, but adds another dependency and review surface.

## Consequences

Operators must monitor `support@grix.im` for deletion requests and complete verified deletions. Redeploy backend so the embedded public page is live before resubmitting Play.

## Verification

- `go test ./internal/publicsite/...` asserts the page exposes `mailto:support@grix.im` and states the request works without reinstalling the app.
- After deploy, confirm `https://gb.grix.im/legal/account-deletion` returns 200 and shows the email request CTA.
