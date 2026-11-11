# Grix Widget Android SDK

[中文说明](#中文说明) | [English](#english)

---

## 中文说明

`grix-widget` 是 Grix 网站挂件移动端（第一期）的原生 Android SDK（Kotlin），支持 **Android 6.0+（minSdk 23）**。SDK 采用薄壳架构：内部通过 Android `WebView` 加载托管页面 `{apiBase}/public/widget/app.html#<启动参数>`，所有聊天 UI、WebSocket 连接管理、断线重连和 Token 自动续期均由托管页面完成。

### 1. 安装方式

#### 方式一：源码模块引入（推荐在仓库内或 Git Submodule 接入）

在宿主项目的 `settings.gradle.kts`（或 `settings.gradle`）中引入模块：

```kotlin
include(":grix-widget")
project(":grix-widget").projectDir = File(rootDir, "path/to/grix/sdk/android")
```

在宿主 `app/build.gradle.kts` 中添加依赖：

```kotlin
dependencies {
    implementation(project(":grix-widget"))
}
```

#### 方式二：构建并引入 AAR 包（Maven 坐标 `im.grix.widget:grix-widget:1.0.0`）

在 `sdk/android` 目录下执行：

```bash
gradle assembleRelease
```

生成的 AAR 文件位于 `sdk/android/build/outputs/aar/grix-widget-release.aar`，可放入宿主项目的 `libs/` 目录引入：

```kotlin
dependencies {
    implementation(files("libs/grix-widget-release.aar"))
}
```

---

### 2. 前置条件：在站点设置中配置 Allowed Apps

在 Grix 客户端打开 **个人页 -> 网站客服 -> 站点设置**，将宿主 App 的 `applicationId`（例如 `com.example.myapp`）添加到 **允许接入的 App（Allowed Apps）** 列表中。未加入白名单的包名在初始化时会收到 `4003 origin not allowed` 并触发 `GrixWidget.onError`。

---

### 3. 所需权限与 AndroidManifest 配置

SDK 的 `AndroidManifest.xml` 已通过 Manifest Merger 自动声明网络与语音通话麦克风权限：

```xml
<uses-permission android:name="android.permission.INTERNET" />
<uses-permission android:name="android.permission.RECORD_AUDIO" />
<uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS" />
```

- **运行时麦克风授权与降级处理**：
  - 当访客在聊天页点击语音通话按钮时，SDK 会在 `WebChromeClient.onPermissionRequest` 中检查 `RECORD_AUDIO` 权限：
    - 若已授权，直接调用 `request.grant(arrayOf(PermissionRequest.RESOURCE_AUDIO_CAPTURE))`；
    - 若未授权，自动通过宿主 `Activity.requestPermissions` 请求运行时 `RECORD_AUDIO` 权限。
  - 若使用 `GrixWidget.present(context)` 打开独立 `GrixWidgetActivity`，权限回调已由 SDK 自动处理。
  - 若使用 `GrixWidget.makeView(context)` 将视图嵌入到你自己的 `Activity` 中，请在宿主 `Activity.onRequestPermissionsResult` 中转发一行调用：
    ```kotlin
    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        GrixWidget.onRequestPermissionsResult(requestCode, grantResults)
    }
    ```
  - 当用户拒绝麦克风权限时，SDK 会优雅调用 `request.deny()`，托管页面提示麦克风不可用但保持会话与文字聊天正常，不会崩溃或跳出页面。

---

### 4. 最小接入代码

```kotlin
import im.grix.widget.GrixUser
import im.grix.widget.GrixWidget

// 1. 初始化配置
GrixWidget.configure(
    siteKey = "wk_your_site_key",
    apiBase = "https://gb.grix.im", // 默认即为线上地址，可省略
    locale = "zh-CN",
    themeColor = "#0f766e",
    title = "在线客服",
    user = GrixUser(name = "张三", email = "zhangsan@example.com")
)

// 2. 全屏 Activity 打开对话页
GrixWidget.present(context)

// 或者：获取可嵌入任意父布局 / Fragment 的 View
val chatView = GrixWidget.makeView(this)
containerLayout.addView(chatView)

// 或者：显示右下角悬浮按钮（颜色跟随 themeColor，自带未读数角标）
GrixWidget.setLauncherVisible(true, activity = this)
```

---

### 5. API 与参数说明

#### `GrixWidget.configure(...)`

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `siteKey` | `String` | 是 | - | Grix 站点唯一标识 `site_key` |
| `apiBase` | `String` | 否 | `"https://gb.grix.im"` | Grix 服务端基础地址 |
| `locale` | `String?` | 否 | `null` | 界面语言（如 `"zh-CN"`、`"en-US"`） |
| `themeColor` | `String?` | 否 | `null` | 主题色（支持 `#RRGGBB` 等 CSS 颜色） |
| `title` | `String?` | 否 | `null` | 聊天顶栏自定义标题 |
| `user` | `GrixUser?` | 否 | `null` | 访客信息 `GrixUser(name, email)` |

> **说明**：SDK 自动通过 `context.packageName` 读取宿主包名作为 `app_id`，并与 `platform=android`、`sdk_version=1.0.0` 及上述配置一起编码到 `#` URL fragment 中，绝不放入 URL query。

#### 界面展示与内嵌 API
- `GrixWidget.present(context)`：启动全屏 `GrixWidgetActivity` 展示聊天页。
- `GrixWidget.makeView(context)`：返回可嵌入宿主任意 `ViewGroup` 或 `Fragment` 的 `GrixWidgetView`。
- `GrixWidget.setLauncherVisible(visible: Boolean, activity: Activity?)`：显示或隐藏右下角悬浮聊天按钮，背景色自动跟随 `themeColor`，未读消息角标自动同步。

---

### 6. 回调说明

```kotlin
GrixWidget.onClose = {
    // 用户点击聊天页关闭按钮或调用 close() 时触发
}

GrixWidget.onUnreadChange = { count ->
    // 未读消息数变化时触发（界面隐藏且收到新消息时递增，界面可见时归零）
}

GrixWidget.onSessionClosed = { reason ->
    // 客服结束会话时触发（例如 reason == "closed"）
}

GrixWidget.onError = { code, message ->
    // 初始化或运行出错时触发（如包名不在 allowed_apps 白名单、站点被禁用等）
}
```

---

### 7. 访客身份持久化（Visitor Persistence）与可见性同步

- **持久化行为**：当托管页面完成初始化并通过 `GrixBridge` 通道下发 `ready(visitor_key, session_id)` 事件时，SDK 会将 `visitor_key` 存入 `SharedPreferences`（文件名 `im.grix.widget.visitor_store`，键名 `visitor_key:{siteKey}`）。后续打开或杀进程重启后，SDK 会自动读取并传入同一个 `visitor_key`，保持访客身份与历史消息不断开。
- **可见性与外链**：
  - 视图附着/分离窗口、`Activity` 前后台切换（`onResume` / `onPause`）时，SDK 自动调用 `window.grixWidget.setVisible(true/false)`。
  - 点击聊天消息中的链接会触发 `open_url` 事件并通过 `Intent(Intent.ACTION_VIEW)` 调用系统浏览器打开，聊天 WebView 本身保持不跳转。

---

### 8. 已知限制

- **本地明文 HTTP 联调**：在 Android 9+（API 28+）上，系统默认禁止明文 HTTP 请求。生产环境使用 `https://gb.grix.im` 不受影响；若在本地联调使用 `http://127.0.0.1:27180`，需在宿主/示例 App 的 `AndroidManifest.xml` 中配置 `android:usesCleartextTraffic="true"`。

---

## English

`grix-widget` is the native Android SDK (Kotlin) for the Grix Website Widget (Phase 1), supporting **Android 6.0+ (`minSdk 23`)**. Built as a thin WebView shell, it loads `{apiBase}/public/widget/app.html#<fragment>` where all chat UI, WebSocket lifecycle, reconnection, and token renewal are handled by the hosted page.

### 1. Installation

#### Option A: Gradle Subproject
In `settings.gradle.kts`:
```kotlin
include(":grix-widget")
project(":grix-widget").projectDir = File(rootDir, "path/to/grix/sdk/android")
```
In `app/build.gradle.kts`:
```kotlin
dependencies {
    implementation(project(":grix-widget"))
}
```

#### Option B: AAR Artifact (`im.grix.widget:grix-widget:1.0.0`)
Build the AAR from `sdk/android`:
```bash
gradle assembleRelease
```
Then include `sdk/android/build/outputs/aar/grix-widget-release.aar` in your host project.

### 2. Prerequisite: Configure `allowed_apps`
Add your Android app's `applicationId` (e.g. `com.example.myapp`) to **Allowed Apps** in Grix Site Settings. Unlisted package names will be rejected with `4003 origin not allowed` via `GrixWidget.onError`.

### 3. Permissions & Microphone Handling
The SDK manifest declares `INTERNET`, `RECORD_AUDIO`, and `MODIFY_AUDIO_SETTINGS`. When a visitor starts a voice call, `WebChromeClient.onPermissionRequest` checks `RECORD_AUDIO` and requests runtime permission if needed. If the user denies the permission, the SDK gracefully calls `request.deny()` so text chat continues uninterrupted.

### 4. Quick Start
```kotlin
GrixWidget.configure(
    siteKey = "wk_your_site_key",
    apiBase = "https://gb.grix.im",
    locale = "en-US",
    themeColor = "#0f766e",
    title = "Support",
    user = GrixUser(name = "Alice", email = "alice@example.com")
)

// Open full-screen chat Activity
GrixWidget.present(context)

// Or embed as a View
val view = GrixWidget.makeView(this)

// Or show the floating launcher button
GrixWidget.setLauncherVisible(true, activity = this)
```

### 5. Callbacks & Persistence
- Callbacks: `onClose`, `onUnreadChange(count)`, `onSessionClosed(reason)`, `onError(code, message)`.
- `visitor_key` is persisted in `SharedPreferences` (`im.grix.widget.visitor_store`) scoped by `siteKey` (`visitor_key:{siteKey}`).
- External links in messages trigger `open_url` and open in the system browser via `Intent.ACTION_VIEW` without navigating the WebView away.
