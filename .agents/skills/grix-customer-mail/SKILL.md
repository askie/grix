---
name: grix-customer-mail
description: Locate a Grix customer from a Sentry alert or connector problem report and prepare a one-to-one fix-notification email. Use when asked to find which user owns a host (by host name, connector version, or upgrade report), draft a customer fix email, or notify users stuck on old grix-connector versions. One-to-one customer mail goes from the monitored support@grix.im mailbox by a human; the Aliyun DirectMail no-reply sender is reserved for verification codes and opt-in announcements and must not be used for one-to-one follow-ups.
---

# Grix Customer Mail

Prepare one-to-one customer fix notifications (for example, telling a user whose
grix-connector is crash-looping how to upgrade). The agent locates the user and
drafts the mail; a human sends it from the monitored support mailbox.

## Channel policy (decided 2026-10-22, extends the 2026-09-06 reach decision)

- **One-to-one customer conversations go out from `support@grix.im`, sent by a
  human.** That mailbox is monitored, so the customer can simply reply.
- **Never send one-to-one follow-ups from the Aliyun DirectMail no-reply sender**
  (`no-reply@mail.dhf.pub`, `SendReachEmail`, `SingleSendMail`, or the
  `/reach/direct` and `/connector/reports/notify` paths). Replies to it are
  never read. That sender is reserved for verification codes and the opt-in
  announcement/marketing flow. See
  `.agents/notes/implemented/2026-09-06-direct-reach-drops-email-channel.md`.
- The agent has no mailbox tool: the deliverable is a final, addressed,
  ready-to-send draft handed to the human, plus the evidence for who it goes to
  and why. Do not script around this with DirectMail credentials.
- Exception: only after telling the user about this policy, and only with their
  explicit instruction to deviate, may a no-reply send happen; then the body
  must direct replies to a new mail to `support@grix.im`, and the deviation and
  the provider request ID must be reported in the handoff.

## Workflow

Copy this checklist and track progress:

```
Task Progress:
- [ ] Step 1: Identify the source event (host, version, error)
- [ ] Step 2: Resolve the host to a user and email in production
- [ ] Step 3: Draft the personalized email
- [ ] Step 4: Hand off for human sending from support@grix.im
```

### Step 1: Identify the source event

From the alert, extract host name, connector release, error text, and event
rate. A steady ~1 event/minute rhythm usually means one machine in a
service-manager restart loop, not a fleet-wide incident; say which it is.
Beware two unreliable labels in connector Sentry events:

- Release `0.1.0` is the hard-coded fallback when the connector cannot find a
  `package.json`; it does not prove an ancient install.
- A recent release label does not prove recent code: 3.x on Windows runs from a
  runtime copy that npm upgrades do not refresh, so the executing code can be
  older than the reported version (check for the fix's presence, for example a
  `Get-CimInstance` fallback, before trusting the label).

### Step 2: Resolve the host to a user (TKE, read-only)

Production is the TKE context in the local kubeconfig; the app namespace is
`clawpool` and Postgres is pod `aibot-postgres-0`. Use the DB role/database
from the `aibot-infra-env` secret **by key name only** — never print secret
values, and run read-only queries only.

Primary path — connector upgrade reports carry the host name:

```sql
SELECT r.host_name, r.from_version, r.to_version, r.status, r.error_code,
       r.agent_id, a.agent_name, a.owner_id, u.nickname, u.email,
       a.connector_version, a.connector_version_seen_at, r.reported_at
FROM connector_upgrade_reports r
LEFT JOIN agents a ON a.id = r.agent_id
LEFT JOIN users u ON u.id = a.owner_id
WHERE r.host_name = '<HOST>'
ORDER BY r.reported_at DESC LIMIT 15;
```

Fallback — users still reporting an old connector version over WS:

```sql
SELECT a.connector_version, a.connector_version_seen_at,
       u.nickname, u.email
FROM agents a JOIN users u ON u.id = a.owner_id
WHERE a.connector_version LIKE '3.%'
ORDER BY a.connector_version_seen_at DESC;
```

Judgement rules:

- Host names are not globally unique (`MacBook-Pro.local` repeats). Confirm the
  owner via `agent_id -> agents.owner_id`, and sanity-check with the agent
  names and `connector_version_seen_at` before naming a recipient.
- If the alert host name is masked (for example `Lxxx`) or the host has no
  platform record (a machine that never registered an agent leaves no row),
  report that it cannot be resolved from this system instead of guessing.
- Show only the recipient(s) needed for the task. Do not dump user tables, and
  never commit addresses, credentials, or query output to the repository.
- One recipient per email. Bulk/stale-version lists are evidence for scoping,
  not a mailing list; mass notification needs its own explicit instruction.

### Step 3: Draft the email

Personalize from Step 2 evidence: host name, stuck version, agent names, and
how long the agents have been offline (`connector_version_seen_at`). Structure:

1. Subject: `【需您操作】` + problem + fix in one line.
2. What we observed on their machine (host, version, affected agents).
3. Root cause in one plain-language paragraph, no internal jargon, no blame.
4. Numbered fix steps with commands in code blocks (see templates.md).
5. What to do if a step fails: since the mail comes from `support@grix.im`,
   the customer can simply reply with the requested log/screenshot.
6. Sign as the Grix support team with the date.

For ready-made bodies, see [templates.md](templates.md) (Windows `wmic`
startup crash; macOS empty-config loop). Adapt, do not send verbatim blind:
versions, paths, and agent names must match the resolved user.

### Step 4: Hand off

Deliver to the user (the human operator):

- The resolved recipient: nickname, email, host, and the query evidence
  (status/error code, last seen). Redact nothing the operator needs, add
  nothing they do not.
- The final subject and body, addressed and ready to paste into the
  `support@grix.im` mailbox.
- Who was **not** contacted and why (masked host, no platform record, no
  email on file), so nobody is silently dropped.
- After the human confirms sending, note the expected effect (for example,
  the Sentry event rate for that host should stop) and when to check back.

## Safety

- Read-only production access only; no `kubectl apply`, no DB writes, no
  secret values in output, logs, or commits.
- Confirm the recipient from at least two signals (host report + agent owner)
  before drafting; a wrong-recipient mail leaks another customer's host and
  agent names.
- Do not use the SMS fallback in the connector problem-user notifier as a
  substitute without explicit instruction (its template is not filed; sends
  report `not_configured`).
