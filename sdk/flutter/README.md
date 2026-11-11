# Grix Widget Flutter SDK

[中文文档](#中文文档) | [English Documentation](#english-documentation)

---

## 中文文档

`GrixWidget` 是 Grix 网站客服组件的官方 Flutter SDK（支持 **Flutter 3.22+ / Dart 3.4+**）。SDK 基于 `webview_flutter` 封装，自动管理 `visitor_key` 的 shared_preferences 持久化、未读消息角标、系统浏览器打开外链以及语音通话的麦克风权限委托。

### 1. 前置准备：配置 `allowed_apps`

在接入前，请先在 Grix 客户端的 **我的 → 网站客服组件 → 站点设置** 中，将你的 Flutter 应用 **Bundle Identifier**（如 `com.example.app`）添加到 **允许的 App（`allowed_apps`）** 白名单中，并获取站点的 `site_key`。

### 2. 安装方式

Add the package to the host app's `pubspec.yaml`:

```yaml
dependencies:
  grix_widget:
    path: path/to/grix/sdk/flutter
```

For a Git dependency:

```yaml
dependencies:
  grix_widget:
    git:
      url: https://github.com/askie/grix.git
      path: sdk/flutter
```

Then run `flutter pub get`. Requirements: Flutter 3.22+ and Dart 3.4+.
### 3. 权限配置（语音通话）

iOS host app `Info.plist`:

```xml
<key>NSMicrophoneUsageDescription</key>
<string>Grix needs microphone access for voice calls with customer support.</string>
```

Android host app `AndroidManifest.xml`:

```xml
<uses-permission android:name="android.permission.INTERNET" />
<uses-permission android:name="android.permission.RECORD_AUDIO" />
<uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS" />
```

The SDK handles `WebViewPermissionRequest` and asks `permission_handler` for the operating-system microphone permission. If the visitor denies it, the SDK denies the WebView capture request and keeps text chat working. If the host uses CocoaPods instead of Swift Package Manager, add `PERMISSION_MICROPHONE=1` to `GCC_PREPROCESSOR_DEFINITIONS` for `permission_handler_apple`.

### 4. 最小接入代码

```dart
import 'package:grix_widget/grix_widget.dart';

GrixWidget.configure(
  siteKey: 'your_site_key',
  apiBase: 'https://gb.grix.im',
  locale: 'zh-CN',
  themeColor: '#0f766e',
  title: 'Online Support',
  user: const GrixUser(name: 'Alice', email: 'alice@example.com'),
);

GrixWidget.onClose = () {};
GrixWidget.onUnreadChange = (count) {};
GrixWidget.onSessionClosed = (reason) {};
GrixWidget.onError = (code, message) {};

// Full-screen route.
await GrixWidget.present(context);

// Embedded widget.
final controller = GrixWidgetController();
final widget = GrixWidget.makeView(controller: controller);

// Floating launcher.
GrixWidget.setLauncherVisible(context, true);
```

### 5. API 与参数说明

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `siteKey` | `String` | Yes | - | Widget site key (`wk_...`) |
| `apiBase` | `String` | No | `https://gb.grix.im` | Grix server base URL |
| `locale` | `String?` | No | `null` | UI locale such as `zh-CN` or `en-US` |
| `themeColor` | `String?` | No | `null` | Theme color; also used by the launcher |
| `title` | `String?` | No | `null` | Custom chat header title |
| `user` | `GrixUser?` | No | `null` | Optional visitor name and email |
| `appIdOverride` | `String?` | No | `null` | Test-only override; normally read from the host app |

- `GrixWidget.present(BuildContext context)`: opens a full-screen route.
- `GrixWidget.makeView({GrixWidgetController? controller})`: returns an embeddable Widget.
- `GrixWidget.setLauncherVisible(BuildContext context, bool visible)`: shows or hides the floating launcher.
- `GrixWidgetController.setVisible(bool)`: notifies the hosted page whether the view is visible.
- `GrixWidgetController.close()`: asks the hosted page to run its normal close flow.

The host bundle id / package name is read with `package_info_plus` and sent as `app_id`. All startup values are placed in the URL fragment (`#`), never in the query string.

### 6. Visitor 持久化与可见性行为

- On `ready`, the SDK persists `visitor_key` under the host site key with `shared_preferences` using the prefix `im.grix.widget.visitor_key.`. The next launch loads the same key before building the URL.
- The SDK calls `window.grixWidget.setVisible(true/false)` when the app lifecycle changes and when a tracked route is pushed or popped. Register `GrixWidget.routeObserver` in `MaterialApp.navigatorObservers` for route-level visibility sync.
- `open_url` links are opened with `url_launcher` in the system browser. The WebView stays on the chat page.

### 7. 已知限制

- iOS 14: WKWebView media capture permission delegation is available from iOS 15. Text chat, unread counts, and history work on iOS 14, but voice capture inside the WebView may not be available.
- Microphone permission still requires the host `Info.plist` / `AndroidManifest.xml` declarations and the normal operating-system consent dialog.
- Local HTTP development requires Android `usesCleartextTraffic="true"` and an iOS ATS exception. Production must use HTTPS.
- Simulator testing can verify that the permission prompt is requested; real-device audio quality requires a physical device.
- Uninstalling the host app clears `shared_preferences`; persist identity across reinstall only if the host restores app data itself.

---

## English Documentation

`grix_widget` is the official Flutter SDK for the Grix Website Widget (live chat + AI/human voice calls). It is a thin-shell wrapper around `webview_flutter`: the hosted page `/public/widget/app.html#<fragment>` owns chat rendering, WebSocket transport, reconnect, token renewal, and voice-call UI. The Flutter package owns the JS bridge, visitor-key persistence, visibility notifications, external-link opening, and microphone permission delegation.

### 1. Prerequisite: Configure `allowed_apps`

In the Grix client, open **My -> Website Widget -> Site Settings** and add the host application's identifiers to **Allowed Apps**:

- iOS: the Xcode `Bundle Identifier` (for example `com.example.app`).
- Android: the Gradle `applicationId` (for example `com.example.app`).
- Flutter: add both the iOS bundle identifier and Android package name when both platforms are supported.

Copy the widget site key (`wk_...`) and pass it to `GrixWidget.configure(siteKey: ...)`.

### 2. Installation

Add the package to your `pubspec.yaml`:

```yaml
dependencies:
  grix_widget:
    path: path/to/grix/sdk/flutter
```

Or use the Git repository:

```yaml
dependencies:
  grix_widget:
    git:
      url: https://github.com/askie/grix.git
      path: sdk/flutter
```

Then run:

```bash
flutter pub get
```

Requirements: Flutter 3.22+ and Dart 3.4+.

### 3. Permissions (Voice Call)

The host app must declare microphone usage. iOS `Info.plist`:

```xml
<key>NSMicrophoneUsageDescription</key>
<string>Grix needs microphone access for voice calls with customer support.</string>
```

Android `AndroidManifest.xml`:

```xml
<uses-permission android:name="android.permission.INTERNET" />
<uses-permission android:name="android.permission.RECORD_AUDIO" />
<uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS" />
```

The SDK handles `WebViewPermissionRequest` and requests the operating-system microphone permission through `permission_handler`. If the visitor denies it, the SDK denies the WebView capture request and keeps text chat usable. If the host uses CocoaPods instead of Swift Package Manager, add `PERMISSION_MICROPHONE=1` to `GCC_PREPROCESSOR_DEFINITIONS` for `permission_handler_apple`.

### 4. Quick Start

```dart
import 'package:grix_widget/grix_widget.dart';

GrixWidget.configure(
  siteKey: 'your_site_key',
  apiBase: 'https://gb.grix.im',
  locale: 'zh-CN',
  themeColor: '#0f766e',
  title: 'Online Support',
  user: const GrixUser(name: 'Alice', email: 'alice@example.com'),
);

GrixWidget.onClose = () {};
GrixWidget.onUnreadChange = (count) {};
GrixWidget.onSessionClosed = (reason) {};
GrixWidget.onError = (code, message) {};

// Full-screen route.
await GrixWidget.present(context);

// Embedded view.
final controller = GrixWidgetController();
final widget = GrixWidget.makeView(controller: controller);

// Optional floating launcher.
GrixWidget.setLauncherVisible(context, true);
```

For route-level visibility tracking, register the SDK observer:

```dart
MaterialApp(
  navigatorObservers: [GrixWidget.routeObserver],
  // ...
)
```

### 5. API Reference

#### `GrixWidget.configure(...)`

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `siteKey` | `String` | Yes | - | Widget site key (`wk_...`) |
| `apiBase` | `String` | No | `https://gb.grix.im` | Grix server base URL |
| `locale` | `String?` | No | `null` | UI locale, e.g. `zh-CN` or `en-US` |
| `themeColor` | `String?` | No | `null` | Theme color used by the page and launcher |
| `title` | `String?` | No | `null` | Custom chat header title |
| `user` | `GrixUser?` | No | `null` | Optional visitor name and email |
| `appIdOverride` | `String?` | No | `null` | Test-only override; normally read from the host app |

The host bundle id / package name is read with `package_info_plus` and sent as `app_id`. All startup values are placed in the URL fragment (`#`), never in the query string. This keeps visitor keys and PII out of HTTP access logs.

#### Presentation and embedding

- `GrixWidget.present(BuildContext context)`: pushes a full-screen `MaterialPageRoute`.
- `GrixWidget.makeView({GrixWidgetController? controller})`: returns a `GrixWidgetView` that can be embedded in any layout.
- `GrixWidget.setLauncherVisible(BuildContext context, bool visible)`: shows/hides a floating launcher with an unread badge.
- `GrixWidgetController.setVisible(bool)`: forwards visibility to `window.grixWidget.setVisible(...)`.
- `GrixWidgetController.close()`: forwards close to `window.grixWidget.close()`.

#### Callbacks

| Callback | Signature | When it fires |
| :--- | :--- | :--- |
| `GrixWidget.onClose` | `void Function()?` | Visitor taps the page close button, or the host calls `controller.close()` |
| `GrixWidget.onUnreadChange` | `void Function(int count)?` | The hosted page emits an updated unread count |
| `GrixWidget.onSessionClosed` | `void Function(String reason)?` | The server closes or bans the visitor session |
| `GrixWidget.onError` | `void Function(String code, String message)?` | Bootstrap or runtime error, e.g. unauthorized app id |

### 6. Visitor Persistence and Visibility

- On the `ready` event the SDK stores `visitor_key` in `shared_preferences`, scoped by `siteKey` with the prefix `im.grix.widget.visitor_key.`. The next launch reads the key before loading the WebView, restoring the same visitor and conversation history after the process is killed.
- The SDK calls `window.grixWidget.setVisible(true/false)` on app lifecycle changes and on tracked route push/pop events. Register `GrixWidget.routeObserver` in `MaterialApp.navigatorObservers` for route-level tracking.
- `open_url` events are opened with `url_launcher` in the system browser. The WebView stays on the chat page and does not navigate away.
- Close, session-closed, unread, and error bridge events are delivered through the callbacks above.

### 7. Example App

A complete host app is in [`sdk/examples/flutter`](../examples/flutter). It demonstrates:

- `present()` full-screen presentation.
- `makeView()` embedding with a `GrixWidgetController`.
- Floating launcher and unread badge.
- Callback logging and local-backend configuration.

Run it with:

```bash
cd sdk/examples/flutter
flutter pub get
flutter run
```

### 8. Known Limitations

- iOS 14: WKWebView media capture permission delegation is available from iOS 15. Text chat, unread counts, and history work on iOS 14, but voice capture inside the WebView may not be available.
- Microphone access still requires the host `Info.plist` / `AndroidManifest.xml` declarations and the normal operating-system consent dialog.
- Local HTTP development requires Android `usesCleartextTraffic="true"` and an iOS ATS exception. Production must use HTTPS.
- Simulator testing can verify the permission prompt path; real-device audio requires a physical device.
- Uninstalling the host app clears `shared_preferences`. Identity survives process restarts, not an app-data wipe.
