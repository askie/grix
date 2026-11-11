# Widget Mobile App Channel and Hosted WebView Contract

- Status: implemented
- Date: 2026-11-11
- Areas: backend, frontend

## Context

The website visitor widget previously relied on a two-part web architecture:

1. `widget.js` runs on the customer's webpage, calls `GET /v1/widget/config` and `POST /v1/widget/visitor/init` with the browser's `Origin` header, persists `visitor_key` in `localStorage`, refreshes short-lived `widget_token` credentials before `expires_in`, and mounts an `iframe` pointing to `/public/widget/frame.html`.
2. `frame.html` receives bootstrap parameters via `window.postMessage` (`grix-widget-init` / `grix-widget-token-refresh`) and manages the WebSocket connection (`/v1/widget/ws`) and conversation UI.

We are introducing thin-shell mobile SDKs for iOS (Swift), Android (Kotlin), and Flutter (pure Dart). Each SDK embeds a system `WebView` that loads a server-hosted conversation page so all chat UI and protocol logic remain on the server. However, native mobile `WebView` requests do not carry a trustworthy website `Origin` header, so the existing `widget_sites.allowed_origins` check in `/v1/widget/visitor/init` and `/v1/widget/config` rejects mobile SDK traffic.

## Decision

### 1. Site Allowlist Extension (`allowed_apps`)

- Migration `132_widget_sites_allowed_apps.sql` adds `allowed_apps JSONB NOT NULL DEFAULT '[]'::jsonb` to `widget_sites`.
- Each entry is an iOS bundle identifier or Android package name normalized to lowercase and validated against reverse-domain format (`^[a-z][a-z0-9_]*(\.[a-z0-9_-]+)+$`, maximum 255 characters).
- A widget site must configure at least one entry in `allowed_origins` or `allowed_apps` (or both).
- Site management endpoints (`POST /v1/widget/sites/create`, `POST /v1/widget/sites/update`, `GET /v1/widget/sites/list`, `GET /v1/widget/sites/detail`) and the Flutter client site settings UI read and write `allowed_apps`.
- In `POST /v1/widget/sites/update`, omitting `allowed_apps` (`nil`) preserves the existing `allowed_apps` stored in the database, and omitting `allowed_origins` (`nil`) preserves the existing `allowed_origins`; only an explicit empty array (`[]`) clears the corresponding list. This ensures legacy clients that do not send `allowed_apps` cannot accidentally wipe mobile app allowlists or fail when editing an App-only site.

### 2. App Validation Channel in `init` and `config`

- `POST /v1/widget/visitor/init` accepts an optional `client` object in the JSON body:
  ```json
  {
    "site_key": "wk_...",
    "visitor_key": "vk_...",
    "visitor_name": "Alice",
    "visitor_email": "alice@example.com",
    "locale": "zh_CN",
    "client": {
      "platform": "ios",
      "app_id": "com.example.shop",
      "sdk_version": "1.0.0"
    }
  }
  ```
- `GET /v1/widget/config` accepts the same optional client fields via query parameters: `platform`, `app_id`, and `sdk_version` (triggers the App validation channel whenever any of the three is non-empty).
- Validation rules:
  - **When `client` is present**:
    - `platform` must be one of `"ios"`, `"android"`, or `"flutter"`.
    - `app_id` must match the reverse-domain format and exist in the site's `allowed_apps` list (case-insensitive).
    - The HTTP `Origin` header is not checked.
    - If `platform` or `app_id` is invalid or `app_id` is not in `allowed_apps`, the server returns the exact same error as an origin mismatch (`ErrWidgetOriginNotAllowed`, HTTP `403`, code `4003`).
  - **When `client` is absent**:
    - Behavior is byte-for-byte identical to the existing web widget (`Origin` header validated against `allowed_origins`).
- First-origin tracking (`widget_sites.first_origin`):
  - On the first successful `POST /v1/widget/visitor/init` for a site over the App channel, `first_origin` is written once (`WHERE first_origin = ''`) as `"app:<app_id>"`.
  - `normalizeOriginHost` and `WidgetSiteUpdate` reverse validation recognize the `"app:"` prefix: if a site update removes that `app_id` from `allowed_apps`, `first_origin` is cleared to `""` so the next valid visitor origin or app can be recorded.
- Fingerprinting and risk audit:
  - In App mode, `page_url` / `last_page_url` is fixed to `"app://<platform>/<app_id>"` (never `window.location.href`).
  - `deriveWidgetVisitorFingerprint` incorporates `platform` and `app_id` alongside the `/24` IP prefix and `User-Agent`.
  - `WriteAuditLog` records `widget_visitor_init` with `platform`, `app_id`, and `sdk_version` in `detail`.

### 3. Security Boundary

- `client.app_id` is a self-declared identifier sent by the client, which can be spoofed by a non-browser HTTP caller just as the `Origin` header can be spoofed outside a browser. Its security level is equivalent to `Origin` allowlisting: it prevents accidental cross-app misconfiguration and casual embedding, not determined abuse.
- The real abuse defenses for both web and mobile channels are:
  1. Owner-scoped IP ban (`IsWidgetIPBanned` / `BanWidgetIP`),
  2. Per-IP and per-fingerprint sliding-window rate limiting (`widgetAllowVisitorInitRate`),
  3. Visitor session ban (`WidgetSessionStatusBanned`).
  All three defenses remain unconditionally active on the App channel.
- Cryptographic visitor identity verification (HMAC signature issued by the customer's backend) is deferred to Phase 2.

### 4. Hosted App Page (`/public/widget/app.html`) and Native Bridge Contract

`/public/widget/app.html` serves the same underlying HTML/JS implementation as `/public/widget/frame.html` (activating app mode when the path ends with `/app.html` or `mode=app` is present in the URL fragment) so conversation rendering, history loading, and WebSocket handling have a single source of truth.

#### URL Fragment Parameters

Mobile SDKs load `/public/widget/app.html#site_key=...&platform=...` with all startup parameters placed in the **URL fragment** (`#...`, parsed via `window.location.hash`). Because URL fragments are never sent over the wire in HTTP requests, session credentials (`visitor_key`) and visitor PII (`visitor_name`, `visitor_email`) never appear in server access logs or reverse-proxy logs:

| Parameter (in `#fragment`) | Required | Description |
| :--- | :--- | :--- |
| `site_key` | Yes | Widget site key (`wk_...`). |
| `platform` | Yes | `"ios"` \| `"android"` \| `"flutter"`. |
| `app_id` | Yes | Host application Bundle ID or package name. |
| `sdk_version` | No | SDK version string (e.g. `"1.0.0"`). |
| `visitor_key` | No | Previously persisted `visitor_key` from native storage. |
| `locale` | No | Preferred locale tag (e.g. `"zh-CN"`, `"en_US"`). |
| `theme_color` | No | Optional hex color override (e.g. `"#0f766e"`). |
| `title` | No | Optional header title override. |
| `visitor_name` | No | Optional visitor display name. |
| `visitor_email` | No | Optional visitor email address. |

#### Self-Bootstrap and Token Refresh Lifecycle

In app mode, `/public/widget/app.html` manages its own session lifecycle without an outer `widget.js` frame:
1. Calls `POST /v1/widget/visitor/init` with `client: {platform, app_id, sdk_version}`, `page_url: "app://<platform>/<app_id>"`, and any supplied visitor parameters.
2. Connects to the returned `ws_url` and authenticates with `widget_token`.
3. Schedules automatic token refresh `max((expires_in - 60) * 1000, 30000)` ms before token expiry by re-calling `/v1/widget/visitor/init`.
4. If WebSocket `auth_ack` fails, automatically re-runs `/v1/widget/visitor/init` once to obtain a fresh token and reconnects.

#### WebView → Native Host Event Bridge

All outbound events from the page are dispatched through a single bridge function (`emitHostEvent(type, extra)`) using a flat JSON object envelope:

```json
{
  "type": "<event_type>",
  "...": "event-specific fields"
}
```

The bridge probes host channels in strict priority order and invokes the first available handler:
1. **iOS (`WKScriptMessageHandler`)**: `window.webkit.messageHandlers.grix.postMessage(payloadObject)` (passes the structured JS object).
2. **Android (`@JavascriptInterface`)**: `window.GrixBridge.postMessage(JSON.stringify(payloadObject))` (passes a JSON string).
3. **Flutter (`webview_flutter` JS channel)**: `window.GrixFlutter.postMessage(JSON.stringify(payloadObject))` (passes a JSON string).

#### Event List (WebView → Host)

| Event `type` | Payload Fields (alongside `type`) | Trigger Condition |
| :--- | :--- | :--- |
| `ready` | `{"type": "ready", "visitor_key": string, "session_id": string}` | Fired after the initial `/v1/widget/visitor/init` succeeds, and again whenever a subsequent init/refresh returns a new `visitor_key`. Native SDKs must persist `visitor_key` in local storage (`UserDefaults` / `SharedPreferences`). |
| `close` | `{"type": "close"}` | Fired when the user taps the close button in the chat header or when `window.grixWidget.close()` is invoked. |
| `session_closed` | `{"type": "session_closed", "reason": string}` | Fired when the server closes or bans the visitor session (`"closed"` or `"banned"`). |
| `unread` | `{"type": "unread", "count": number}` | When the page is marked hidden (`setVisible(false)`), each incoming non-visitor message increments the unread counter and emits `unread`. Calling `setVisible(true)` resets the counter to `0` and emits `{"type": "unread", "count": 0}`. |
| `open_url` | `{"type": "open_url", "url": string}` | Fired when the user taps an `http://` or `https://` link inside a chat message. Navigation inside the WebView is prevented so the host SDK can open the URL in the system browser. |
| `error` | `{"type": "error", "code": string, "message": string}` | Fired when bootstrap parameters are missing (`invalid_params`), `/v1/widget/visitor/init` fails (`init_failed` or server error code), or response payload is invalid (`invalid_response`). |

#### Host → WebView JS API

`/public/widget/app.html` exposes `window.grixWidget` for the native host to call via `evaluateJavascript`:

- `window.grixWidget.setVisible(visible: boolean)`: Informs the page whether the WebView container is currently visible to the user. Passing `true` clears the unread count to `0` (and emits `unread {count: 0}` if it was non-zero) and focuses the message input.
- `window.grixWidget.close()`: Triggers the same close action as tapping the header close button and emits `close {}`.

## Alternatives

- **Separate HTML/JS bundle for mobile (`app.html` + `app.js`)**: Rejected because duplicating the WebSocket state machine, message renderer, and i18n copy from `frame.html` would cause immediate drift between web and mobile widgets.
- **Custom HTTP header for `app_id`**: Rejected because `GET /v1/widget/config` and `POST /v1/widget/visitor/init` are called directly from browser `fetch` inside the WebView, where query parameters and JSON body fields avoid extra CORS preflight surface and work identically across iOS, Android, and Flutter WebViews.

## Consequences

- Mobile SDKs can be implemented as thin native wrappers around `/public/widget/app.html` with a stable, documented event and method contract.
- Existing web widget embeds (`widget.js` + `frame.html`) are completely unaffected when `client` is omitted.
- Site owners can manage both web domains (`allowed_origins`) and mobile applications (`allowed_apps`) from the same site settings dialog.

## Verification

- Backend unit tests:
  - `cd backend && go test ./internal/api/service ./internal/api/handler ./internal/publicsite -v -run "TestWidget|TestNormalizeOriginHost|TestServe"`
- PostgreSQL migration verification (`132_widget_sites_allowed_apps.sql` on legacy and empty schemas):
  - `cd backend && AIBOT_TEST_PG_MIGRATION_DSN="host=/tmp user=gcf dbname=postgres sslmode=disable" go test -tags pgverify ./internal/store -run "TestWidgetSite" -v`
- Frontend widget tests:
  - `cd frontend && flutter test test/modules/profile/widget_sites_mobile_layout_test.dart test/modules/profile/widget_sites_view_test.dart test/modules/profile/widgets/widget_site_form_dialog_test.dart`
