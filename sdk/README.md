# Grix Website Widget Mobile SDKs / Grix 网站客服组件移动端 SDK

Grix 移动端 SDK 允许你将 Grix 网站客服组件（在线聊天 + 语音通话）嵌入到原生 iOS、原生 Android 以及 Flutter 应用中。三个 SDK 均采用统一的轻量托管架构：通过系统 WebView 加载 `{apiBase}/public/widget/app.html#<启动参数>`，由托管页面统一承载聊天界面、WebSocket 连接、自动重连、Token 续期与语音通话逻辑，SDK 负责原生桥接、访客身份持久化、系统浏览器跳转与麦克风权限授权。

Grix Mobile SDKs let you embed the Grix Website Widget (live chat + AI/human voice calls) into native iOS, native Android, and Flutter apps. All three SDKs share a thin-shell architecture: they load `{apiBase}/public/widget/app.html#<fragment_params>` inside a native WebView, delegating chat UI, WebSocket transport, auto-reconnection, token renewal, and voice calls to the hosted page while handling native bridge events, `visitor_key` persistence, external link opening, and microphone permissions.

---

## 1. 平台选择 / Choosing a Platform

| 平台 / Platform | 适用场景 / When to Use | 目录与文档 / Directory & Docs | 示例工程 / Example App |
| :--- | :--- | :--- | :--- |
| **iOS (Swift)** | 原生 iOS 应用（UIKit / SwiftUI），iOS 14.0+，支持 Swift Package Manager 与 CocoaPods | [`sdk/ios/README.md`](./ios/README.md) | [`sdk/examples/ios`](./examples/ios) |
| **Android (Kotlin)** | 原生 Android 应用，minSdk 23（Android 6.0+），支持 Gradle AAR 依赖 | [`sdk/android/README.md`](./android/README.md) | [`sdk/examples/android`](./examples/android) |
| **Flutter (Dart)** | 跨平台 Flutter 应用（iOS & Android），纯 Dart 实现（基于 `webview_flutter`） | [`sdk/flutter/README.md`](./flutter/README.md) | [`sdk/examples/flutter`](./examples/flutter) |

---

## 2. 在站点设置中配置 `allowed_apps` / Configuring `allowed_apps` in Site Settings

移动端 App 没有浏览器 `Origin` 请求头，Grix 服务端通过站点设置中的 **允许的 App（`allowed_apps`）** 白名单校验请求来源：

1. 打开 Grix 客户端，进入 **我的 → 网站客服组件（Website Widget）**。
2. 新建或编辑已有站点，在 **允许的 App（Bundle ID / 包名）** 字段中填写你的应用标识（每行一个或用逗号分隔）：
   - **iOS**：填写 Xcode 工程的 `Bundle Identifier`（如 `com.example.myapp`）。
   - **Android**：填写 Gradle 的 `applicationId`（如 `com.example.myapp`）。
   - **Flutter**：同时填写 iOS 的 `Bundle Identifier` 和 Android 的 `applicationId`。
3. 保存后复制站点的 **Site Key**，传入各端 SDK 的 `configure(siteKey: ...)`。

> **注意**：`allowed_apps` 比对时统一按小写匹配，不支持通配符。如果未配置或传入的 `app_id` 不在白名单中，SDK 的 `onError` 回调会收到 `app_not_allowed` 错误。

---

## 3. 三端统一 API 概览 / Unified API Overview

所有三个平台均提供一致的核心能力：

- **`configure(siteKey, apiBase, locale, themeColor, title, user)`**：初始化站点配置与访客资料（默认 `apiBase` 为 `https://gb.grix.im`）。
- **`present()`**：以弹窗/新页面方式打开客服对话（iOS Modal `UIViewController`、Android `Activity`、Flutter `Navigator.push`）。
- **`makeView()`**：返回可嵌入宿主自定义布局的视图组件（iOS `UIView` / `UIViewController`、Android `View` / `Fragment`、Flutter `Widget`）。
- **`setLauncherVisible(bool)`**：显示或隐藏右下角悬浮客服按钮，背景色自动跟随 `themeColor`，未读消息角标自动更新。
- **事件回调**：
  - `onClose`：访客点击关闭按钮时触发。
  - `onUnreadChange(count)`：未读消息数变化时触发。
  - `onSessionClosed(reason)`：客服结束会话时触发。
  - `onError(code, message)`：初始化失败、App 不在白名单或网络异常时触发。
