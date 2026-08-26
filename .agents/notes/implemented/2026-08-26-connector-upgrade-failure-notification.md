# Connector upgrade failure notification

## Context

`connector_upgrade_reports` accumulates one row per upgrade attempt, so a single machine
can appear as failed, rolled back, and later successful. Admins needed a way to reach the
humans behind those machines, but the admin report list only exposed `agent_id`, phone
numbers are stored encrypted (`users.phone_cipher` plus a plaintext `phone_last4`), and
AliCloud DirectMail's `SingleSendMail` does not accept a `TemplateId`.

## Decision

- `GET /admin/api/connector/reports/problem-users` resolves reports to owners. A machine is
  keyed by `install_id`, then `host_name`, then `agent_id`; only its latest report for the
  requested version counts, and it is dropped when the same machine later reported
  `success`/`installed` on any version. `WINDOWS_UPGRADE_UNSUPPORTED` is excluded unless
  `include_unsupported` is set, because an unsupported platform is not a failure.
- Phone numbers are never returned in plaintext. The list returns `****8000`; the real
  number is decrypted server-side only at send time.
- `POST /admin/api/connector/reports/notify` sends per user with the idempotency key
  `connector_upgrade:{version}:{user_id}:{channel}`, reusing `reach_tasks` /
  `reach_send_logs`. Sending is manual and explicitly selected — there is no automatic blast.
  The key is claimed before delivery, so a task left in `failed` is reopened on the next
  click and its send log row is reused; only a task that reached `sent` or is still
  `sending` reports `duplicate`. Without that, a first attempt made before the SMS template
  was registered would block that version's recipients permanently. Reopening is a
  conditional update (`WHERE id = ? AND status = 'failed'`) so two concurrent clicks cannot
  both enter delivery and send twice.
- Email uses `DescTemplate` to fetch the registered template body (cached 5 minutes),
  substitutes `{name}` and `{body}`, and sends through `SingleSendMail`. The template ID is
  configurable via `ali_email.reach_template_id`.
- Notification SMS uses its own AliCloud template (`aliyun.template_code_notify`) and never
  falls back to the marketing template, because the two template classes are registered
  under different rules. Template 440876 is mail-only (`TemplateType=0`) and carries no SMS
  content, so there is nothing to fall back to. On AWS SNS the notify kind publishes as
  `Transactional` for the same reason. An unset template yields a `not_configured` result
  rather than a silent failure, and the preview runs the same provider resolution as a real
  send, so it cannot report a channel as available that would fail on send.
- Reading the list keeps the existing `connector` permission; sending additionally requires
  `app`, matching the other outbound-messaging endpoints.

## Alternatives

- `BatchSendMail` supports templates directly but requires pre-built recipient lists, so it
  cannot address a single ad-hoc address.
- Reusing `template_code_marketing` for notifications would have avoided a new setting, but
  mixing notification content into a marketing template violates the provider's registration
  terms.
- Aggregating in SQL was rejected: the version filter already narrows the table to a small
  set, and the self-heal rule spans versions, which is clearer in application code.
- Matching self-heal reports by `agent_id` alone was rejected: a machine that re-registers
  its agent would keep looking broken. The self-heal pass matches on `install_id`
  globally, since an install id identifies one installation, and within the owner's own
  agents on `agent_id` or `host_name`. Host names are not globally unique (`localhost`,
  `MacBook-Pro.local`), so an unscoped host-name match would let one account's success
  report silently suppress another account's notification.
- This feature keys a machine with its own `problemHostKey` rather than the shared
  `hostKeyOf`: the host-name fallback includes `agent_id`. Under the shared key two
  accounts owning same-named machines collapse into one entry, which only inflates a
  statistic elsewhere but here drops a real recipient.

## Consequences

- `SmsSettings.Aliyun` gained `template_code_notify`; an existing deployment reads it as
  empty until an operator fills it in, and SMS notification reports `not_configured` until then.
- `MarketingSmsRequest.Kind` is additive; an empty value keeps the previous marketing behavior.
- Repeated clicks on the same version/channel are safe: the second call returns `duplicate`.

## Verification

- `backend/internal/api/service/connector_upgrade_failure_notify_service_test.go` covers the
  per-machine dedupe, cross-version self-heal (including agent re-registration,
  host-name-only matching, and the owner scope), unsupported filtering, template rendering,
  idempotency, retry after a failed task, the concurrent-reopen gate, clamped paging, auto
  channel fallback, and the `not_configured` path.
- `backend/internal/api/service/identity/phone_sms_cn_notify_test.go` locks the notify
  template never falling back to the marketing template.
- `backend/internal/admin/router_api_connector_notify_test.go` asserts the new routes register
  alongside the existing `/connector/reports` route.
