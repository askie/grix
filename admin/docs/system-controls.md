# System controls (ADMIN-REG-01)

The Admin **系统设置 → 系统控制** page is a separate models/service/controller/
binding/view module. It uses the existing responsive Admin scaffold. Draft values
are separate from confirmed values. Loading and saving are serialized, editing is
disabled during either operation, and refresh preserves unsaved drafts. Failed
loads retain the last confirmed data with an error and retry; failed saves never
promote the draft to confirmed data. Leaving the page discards unsaved drafts.
Refresh or re-entry reads the canonical database value, including legacy Feature
Gates updates. A page is not a live subscription to other administrators' edits.

## API and permissions

All responses use the existing `{code, msg, data}` envelope. Both routes run
`RequireAPIAuth` and `RequirePermission("settings")`:

- `GET /admin/api/settings/system-controls` → `data.items`, in directory order.
- `PUT /admin/api/settings/system-controls/:key` → the committed item in `data`.

An item contains `key`, `label`, `description`, `value_type`, and `value`:

```json
{"key":"registration_enabled","label":"允许用户注册","description":"控制新账号开户",
 "value_type":"boolean","value":false}
```

The only current key is `registration_enabled`. Its PUT body is exactly
`{"value":true}` or `{"value":false}`. Unknown keys, missing/null/non-boolean
values, duplicate or extra fields, trailing JSON, malformed bodies, and bodies
larger than 4 KiB are rejected with HTTP 400. Reads or persistence/audit failures
return HTTP 500. Authentication/permission failures return HTTP 401/403.

## Storage and admission

`registration_enabled` is an API name, never another stored setting. Its sole
source is `feature_gates.auth_register`: only `enabled` means true. Missing,
`disabled`, legacy `whitelist`, or unknown status means false. Reading never
creates a row or resets a stored value. Explicit save atomically inserts if
missing, locks the row, changes only its status, and writes an
`AdminOperationLog` in the same transaction (`system_control_update`, operator,
key, before/after boolean values, client IP and user agent). An audit error rolls
back the entire change, including a first insert. Repeated saves keep one gate.
Legacy Feature Gates permissions and storage remain unchanged.

`featuregate.RegistrationEnabled` reads the canonical gate directly from the
primary database on each new account admission and explicit registration
verification-code request. It never uses the process feature cache or its stale
fallback, and DB errors deny admission. Other feature gates retain their cache.
A local cache invalidation after Admin save is only a UI convenience.

After a committed close, any subsequent admission check on another instance
observes the closed gate. A request that already passed admission may still
finish its transaction after close. There is no distributed lock or cancellation
of in-flight requests. Deploy the admission change to every backend instance
before relying on this guarantee; old binaries can still use their old cache.

Existing password, phone, Google and Apple accounts can still log in. Verified
OAuth email recognition/binding and password reset precede or bypass admission.
Phone registration additionally requires the region's SMS registration setting;
phone login remains independent. Sending a `login` SMS may still be necessary
for existing users, but an unknown phone cannot create an account while closed.

## Public client contract

`GET /v1/auth/methods?region=cn|global` adds boolean `registration_enabled`.
`phone_register_enabled` is the conjunction of the global admission gate and
regional SMS registration setting; `phone_login_enabled` is unchanged. A gate
read error closes registration while preserving independently read login
capability. SMS read errors disable SMS capabilities. No parameter directory is
published. Old clients ignore the extra field; new clients tolerate an older
server omitting it, with server admission remaining authoritative.

User APP login and direct registration pages consume this capability. The latter
shows loading, closed, or failed/retry state and blocks registration/code actions
while unavailable. Region/request identity guards discard late capability replies
and old-region verification-code results, including switching away and back.
Region selection is disabled during account-grant submission to prevent applying
a successful session to a different endpoint. OAuth buttons remain available to
existing accounts according to their existing login feature gates. Existing UI
feature snapshots may be stale until refreshed; they never authorize admission.

## Adding a parameter

1. Add an ordered definition to `systemControlDirectory` with a stable key,
   label, description and `value_type`, plus typed read/validate/write functions.
2. Choose its canonical storage. New JSON settings may reuse
   `model.SystemSetting`; never mirror an existing setting into a second source.
3. Make the writer lock/read its previous value and return it for the shared audit
   transaction. Define missing/error semantics and any caching policy explicitly.
4. Add a typed client editor and service update signature if needed. Current
   clients edit booleans only and render unsupported types as read-only.
5. Test validation, persistence, rollback, permissions, UI failure/draft states,
   and all policy consumers. Extend only the necessary public capability fields.

## Independent regression

Use Go 1.26 with CGO/SQLite support and Flutter dependencies from each lockfile
(validated with Flutter 3.47.5 / Dart 3.13.4). Unit tests use isolated SQLite and
miniredis plus fake OAuth token validators; no production credentials, SMS/email
provider or production switch is needed. Do not enable optional live-provider
test flags. The full Go offline suite explicitly disables/skips live E2E, even
when the host environment has enabled it. From each indicated repository subdirectory:

```sh
# backend
GRIX_LIVE_E2E=0 AIBOT_TEST_NATS_URL=nats://127.0.0.1:1 go test -p 1 ./... -skip '^TestLive'
go vet ./...
go build ./...
# admin
flutter pub get
flutter test
flutter analyze --no-fatal-infos
# frontend
flutter pub get
flutter test --timeout 90s
flutter analyze --no-fatal-infos
# repository root
gitleaks detect --source .
```

Focused evidence is in `router_api_system_controls_test.go`,
`registration_policy_test.go`, Admin `system_controls_test.dart`, and User APP
`auth_methods_test.dart`, auth controller and view tests. The admission matrix
includes enabled/disabled/missing/whitelist/DB error, an independent DB connection
update after warming enabled cache, unchanged related row counts on denial,
existing account login/binding/reset, and global × regional SMS conjunction.
Widget evidence covers 390 px and 1280 px Admin widths. PostgreSQL row locking and
real devices/providers are not exercised by these isolated unit tests.
