# Connector upgrade failure notification

## Context

Upgrade reports are attempt history, not a list of currently broken machines.
Operator-selected notifications must identify the affected owners without
cross-account suppression, plaintext phone exposure or accidental repeat sends.

## Decision

- `GET /admin/api/connector/reports/problem-users` counts only the latest
  report per machine for the requested version; later `success`/`installed`
  on any version removes it. Key by `install_id`, then host name, then agent
  ID. `WINDOWS_UPGRADE_UNSUPPORTED` is excluded unless requested explicitly.
- Self-heal matches installation ID globally, but host/agent matches only
  within the owner's agents. Host names are not globally unique. The local
  `problemHostKey` includes agent ID in its host fallback so two owners of
  same-named machines cannot suppress each other's recipient.
- Phone numbers are masked in list responses; decrypt only server-side at send.
- `POST /admin/api/connector/reports/notify` is manual, for explicitly selected
  users, with `connector_upgrade:{version}:{user_id}:{channel}` idempotency
  across `reach_tasks` / `reach_send_logs`. Claim before delivery. `sent` and
  `sending` are duplicates; `failed` may reopen through a conditional
  `WHERE status='failed'` update and reuse its send log. Concurrent retries
  must not both enter delivery.
- Current channels are explicit `sms` or `auto` (SMS only); omitted/invalid
  channel is a request error. Email/template sending was removed by
  [the no-reply channel decision](2026-09-06-direct-reach-drops-email-channel.md).
  There is no automatic blast or silent SMS default.
- Notify SMS uses `aliyun.template_code_notify`, never the marketing template;
  AWS SNS notify uses `Transactional`. Missing provider/template configuration
  returns `not_configured`. Preview uses the same provider resolution as send,
  not just a non-empty template check.
- Reading requires existing `connector` permission; sending additionally
  requires `app`, matching other outbound-messaging endpoints.

## Alternatives

- SQL-only aggregation: rejected; cross-version self-heal and owner-scoped
  fallback matching are clearer after the version filter narrows the reports.
- Agent ID alone: misses machines that re-register an agent.
- Shared host-name-only key/global host matching: can drop another owner's
  real recipient. Reusing a marketing SMS template also violates the intended
  provider class and is not an availability fallback.

## Consequences

Repeated successful sends are suppressed; failed configuration can be fixed and
retried with the same key. Empty notify settings fail visibly instead of silently
falling back. `MarketingSmsRequest.Kind` is additive; empty keeps marketing
behavior. Do not infer deployed SMS configuration from the presence of code.

## Verification

Paths from repository root:

- `backend/internal/api/service/connector_upgrade_failure_notify_service{,_test}.go`:
  machine/owner dedupe, cross-version self-heal/re-registration, unsupported
  filtering, masking, paging, conditional retry/idempotency, explicit channel,
  SMS-only auto, matching preview/send configuration and no email delivery.
- `backend/internal/api/service/identity/phone_sms_cn_notify_test.go`:
  notify never falls back to marketing template.
- `backend/internal/admin/router_api_connector_notify_test.go`: routes and
  existing report endpoint registration.
