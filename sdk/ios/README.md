# GrixWidget iOS SDK

[中文文档](#中文文档) | [English Documentation](#english-documentation)

---

## 中文文档

`GrixWidget` 是 Grix 网站客服组件的官方 iOS SDK（支持 **iOS 14.0+**）。SDK 基于 `WKWebView` 封装，自动管理 `visitor_key` 的 Keychain 持久化、未读消息角标、系统浏览器打开外链以及语音通话的麦克风权限委托。

### 1. 前置准备：配置 `allowed_apps`

在接入前，请先在 Grix 客户端的 **我的 → 网站客服组件 → 站点设置** 中，将你的 iOS 应用 **Bundle Identifier**（如 `com.example.app`）添加到 **允许的 App（`allowed_apps`）** 白名单中，并获取站点的 `site_key`。

### 2. 安装方式

#### 方式一：Swift Package Manager（推荐）

在 Xcode 中选择 **File → Add Package Dependencies...**，输入仓库地址：

```text
https://github.com/askie/grix
```


在 Xcode 的 Add Package 面板中请选择 **Branch: main** 或 **Revision**，不要选择 Version 规则；当前 grix 仓库的 tag 属于 Grix 主工程，不是 GrixWidget 的发布版本。

或在你的 `Package.swift` 中添加依赖：

```swift
dependencies: [
    .package(url: "https://github.com/askie/grix.git", branch: "main")
],
targets: [
    .target(
        name: "YourApp",
        dependencies: [
            .product(name: "GrixWidget", package: "grix")
        ]
    )
]
```

#### 方式二：CocoaPods

在你的 `Podfile` 中添加：

```ruby
platform :ios, '14.0'
use_frameworks!

target 'YourApp' do
  pod 'GrixWidget', :git => 'https://github.com/askie/grix.git', :branch => 'main'
end
```

然后执行：

```bash
pod install
```

### 3. 权限配置（语音通话）

若你的客服站点开启了语音通话能力，宿主 App 的 `Info.plist` **必须**声明麦克风权限描述，否则发起语音通话时系统会拒绝授权：

```xml
<key>NSMicrophoneUsageDescription</key>
<string>需要使用麦克风以便与客服进行语音通话</string>
```

SDK 只会在 `apiBase` 源一致时处理网页麦克风权限申请；其他源的导航会被取消并交给系统浏览器。SDK 不会主动修改宿主 App 的 `AVAudioSession` category；如宿主为通话自行设置，通话结束后请恢复原 category。

### 4. 最小接入代码

```swift
import UIKit
import GrixWidget

// 1. 初始化配置（通常在 AppDelegate / SceneDelegate 或页面初始化时调用）
GrixWidget.configure(
    siteKey: "your_site_key",
    apiBase: "https://gb.grix.im", // 默认即为 https://gb.grix.im，可省略
    locale: "zh-CN",
    themeColor: "#0f766e",
    title: "在线客服",
    user: GrixUser(name: "张三", email: "zhangsan@example.com")
)

// 2. 监听事件回调（可选）
GrixWidget.onUnreadChange = { count in
    print("未读消息数: \(count)")
}
GrixWidget.onClose = {
    print("客服窗口已关闭")
}
GrixWidget.onSessionClosed = { reason in
    print("会话已结束: \(reason)")
}
GrixWidget.onError = { code, message in
    print("客服组件错误 [\(code)]: \(message)")
}

// 3A. 方式一：模态弹出对话窗口
GrixWidget.present()

// 3B. 方式二：显示右下角悬浮客服按钮（点击自动调用 present()）
GrixWidget.setLauncherVisible(true)

// 3C. 方式三：获取可嵌入任意父视图的 UIView 或 UIViewController
let chatView: UIView = GrixWidget.makeView()
let chatVC: UIViewController = GrixWidget.makeViewController()
```

### 5. API 与参数说明

#### `GrixWidget.configure(...)`

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| `siteKey` | `String` | 是 | - | 站点唯一标识（Site Key） |
| `apiBase` | `String` | 否 | `"https://gb.grix.im"` | Grix 服务端地址 |
| `locale` | `String?` | 否 | `nil` | 界面语言（如 `"zh-CN"`、`"en-US"`），不传则跟随系统语言 |
| `themeColor` | `String?` | 否 | `nil` | 主题色（6 位十六进制颜色，如 `"#0f766e"`），悬浮按钮颜色也会同步跟随 |
| `title` | `String?` | 否 | `nil` | 顶部栏自定义标题 |
| `user` | `GrixUser?` | 否 | `nil` | 预填访客信息 `GrixUser(name: String?, email: String?)` |

> **说明**：所有启动参数均通过 URL Fragment（`#`）传递给 `/public/widget/app.html`，绝不通过 URL Query 传输。宿主 App 的 Bundle Identifier 会自动通过 `Bundle.main.bundleIdentifier` 读取并转为小写作为 `app_id` 传入。

#### 界面展示与嵌入

- **`GrixWidget.present(from: UIViewController? = nil, animated: Bool = true)`**：以 Modal 页面弹出客服对话。若不传 `from`，SDK 会自动查找当前前台顶层 `UIViewController`。
- **`GrixWidget.makeView() -> UIView`**：返回可嵌入宿主自定义布局的 `UIView`。
- **`GrixWidget.makeViewController() -> UIViewController`**：返回可嵌入 `UINavigationController` 或 `UITabBarController` 的 `UIViewController`。
- **`GrixWidget.setLauncherVisible(_ visible: Bool)`**：在当前主窗口右下角显示或隐藏悬浮按钮，按钮背景色跟随 `themeColor`，并自动展示未读角标。

#### 回调事件

| 回调属性 | 签名 | 触发时机 |
| :--- | :--- | :--- |
| `GrixWidget.onClose` | `(() -> Void)?` | 访客点击对话页关闭按钮时触发（若由 `present()` 弹出，SDK 会自动关闭弹窗） |
| `GrixWidget.onUnreadChange` | `((Int) -> Void)?` | 当对话处于不可见/后台状态收到新消息、或回到可见状态清零时触发 |
| `GrixWidget.onSessionClosed` | `((String) -> Void)?` | 客服在后台主动结束当前会话时触发（参数为结束原因，如 `"closed"`） |
| `GrixWidget.onError` | `((String, String) -> Void)?` | 发生错误时触发，首个参数为错误码（如 `"app_not_allowed"`、`"site_disabled"`、`"network_error"`），第二个参数为描述信息 |

### 6. Visitor 持久化与可见性行为

- **Keychain 持久化**：当 WebView 首次初始化完成并派发 `ready` 事件时，SDK 会将服务端分配的 `visitor_key` 按 `siteKey` 隔离存储在系统 **Keychain**（服务名 `im.grix.widget.visitor`）中。即使杀掉进程或重装（同签名下）再次打开，也会自动携带该 `visitor_key` 恢复同一个访客身份与历史聊天记录。
- **可见性与前后台同步**：当视图添加到窗口/移出窗口、`UIViewController` 显示/隐藏、以及 App 切换到后台或回到前台（`UIApplication.willEnterForegroundNotification`）且对话页正在显示时，SDK 会自动调用 `window.grixWidget.setVisible(true/false)`，确保未读计数准确。
- **外部链接处理**：收到 `open_url` 事件或访客点击消息中的 `http/https` 链接时，SDK 会调用 `UIApplication.shared.open` 使用系统 Safari 浏览器打开，客服对话页本身不会发生页面跳转。

### 7. 已知限制

- **iOS 14 语音通话限制**：`WKUIDelegate` 的网页麦克风权限委托接口 `webView(_:requestMediaCapturePermissionFor:initiatedByFrame:type:decisionHandler:)` 仅在 **iOS 15.0 及以上**提供。在 iOS 14 设备上，文字聊天、未读角标、历史记录等功能完全正常，但 WebView 内无法直接授权网页麦克风进行语音通话。


- 后续会提供专用镜像仓库和语义化版本。


## English Documentation

`GrixWidget` is the official iOS SDK for the Grix Website Widget (supporting **iOS 14.0+**). Built as a thin wrapper around `WKWebView`, it handles `visitor_key` persistence in the iOS Keychain, unread message badges, opening external links in Safari, and microphone permission delegation for live voice calls.

### 1. Prerequisite: Configure `allowed_apps`

Before integrating, open the Grix client, navigate to **Profile → Website Widget → Site Settings**, add your iOS app's **Bundle Identifier** (e.g. `com.example.app`) to **Allowed Apps (`allowed_apps`)**, and copy your `site_key`.

### 2. Installation

#### Option A: Swift Package Manager (Recommended)

In Xcode, select **File → Add Package Dependencies...** and enter:

```text
https://github.com/askie/grix
```

In the Add Package panel choose **Branch: main** or a **Revision**, not a Version rule. The existing tags in the grix repository belong to the main Grix product, not GrixWidget releases.

Or add it to your `Package.swift`:

```swift
dependencies: [
    .package(url: "https://github.com/askie/grix.git", branch: "main")
],
targets: [
    .target(
        name: "YourApp",
        dependencies: [
            .product(name: "GrixWidget", package: "grix")
        ]
    )
]
```

#### Option B: CocoaPods

Add the following to your `Podfile`:

```ruby
platform :ios, '14.0'
use_frameworks!

target 'YourApp' do
  pod 'GrixWidget', :git => 'https://github.com/askie/grix.git', :branch => 'main'
end
```

Then run `pod install`.

### 3. Permissions (Voice Call)

To support live voice calls, your host app's `Info.plist` **must** include `NSMicrophoneUsageDescription`:

```xml
<key>NSMicrophoneUsageDescription</key>
<string>Microphone access is required for live voice support calls.</string>
```

The SDK only handles media capture permission requests from the configured `apiBase` origin; other top-level navigations are cancelled and opened in the system browser. The SDK does not change the host app's `AVAudioSession` category. If the host changes it for calls, restore the previous category after the call.

### 4. Quick Start

```swift
import UIKit
import GrixWidget

// 1. Configure the SDK
GrixWidget.configure(
    siteKey: "your_site_key",
    apiBase: "https://gb.grix.im",
    locale: "en-US",
    themeColor: "#0f766e",
    title: "Support",
    user: GrixUser(name: "Alice", email: "alice@example.com")
)

// 2. Register callbacks (optional)
GrixWidget.onUnreadChange = { count in
    print("Unread count: \(count)")
}
GrixWidget.onClose = {
    print("Widget closed")
}
GrixWidget.onSessionClosed = { reason in
    print("Session closed: \(reason)")
}
GrixWidget.onError = { code, message in
    print("Widget error [\(code)]: \(message)")
}

// 3A. Present modally
GrixWidget.present()

// 3B. Or show the floating launcher button
GrixWidget.setLauncherVisible(true)

// 3C. Or embed directly into your own view hierarchy
let chatView: UIView = GrixWidget.makeView()
let chatVC: UIViewController = GrixWidget.makeViewController()
```

### 5. API Reference

#### `GrixWidget.configure(...)`

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `siteKey` | `String` | Yes | - | Your Grix Widget Site Key |
| `apiBase` | `String` | No | `"https://gb.grix.im"` | Grix server base URL |
| `locale` | `String?` | No | `nil` | UI locale (e.g. `"en-US"`, `"zh-CN"`) |
| `themeColor` | `String?` | No | `nil` | Hex theme color (e.g. `"#0f766e"`) |
| `title` | `String?` | No | `nil` | Custom header title |
| `user` | `GrixUser?` | No | `nil` | Optional visitor info `GrixUser(name: String?, email: String?)` |

All startup parameters are passed via URL fragment (`#`) to `/public/widget/app.html` and never placed in the URL query string. The host app's `Bundle.main.bundleIdentifier` is automatically normalized to lowercase and passed as `app_id`.

#### Presentation & Embedding

- **`GrixWidget.present(from:animated:)`**: Presents the chat interface modally.
- **`GrixWidget.makeView() -> UIView`**: Returns an embeddable `UIView`.
- **`GrixWidget.makeViewController() -> UIViewController`**: Returns an embeddable `UIViewController`.
- **`GrixWidget.setLauncherVisible(_ visible: Bool)`**: Toggles the floating launcher button on the active window.

#### Callbacks

- **`GrixWidget.onClose`**: Invoked when the visitor closes the widget.
- **`GrixWidget.onUnreadChange`**: Invoked with the latest unread message count (`Int`).
- **`GrixWidget.onSessionClosed`**: Invoked when the support session is closed (`reason: String`).
- **`GrixWidget.onError`**: Invoked on errors (`code: String, message: String`, e.g., `app_not_allowed`).

### 6. Visitor Persistence & Visibility Sync

- **Keychain Persistence**: Upon receiving the `ready` event from the hosted page, `visitor_key` is stored in the iOS **Keychain** scoped by `siteKey`. Subsequent launches automatically reuse the stored `visitor_key` so conversation history is preserved across app restarts.
- **Visibility Sync**: The SDK automatically invokes `window.grixWidget.setVisible(true/false)` when the chat view appears/disappears and when the app returns to the foreground (`UIApplication.willEnterForegroundNotification`) while the chat view is visible.
- **External Links**: `open_url` bridge events and external link clicks are opened in Safari via `UIApplication.shared.open` while keeping the chat WebView intact.

### 7. Known Limitations

- **Voice Calls on iOS 14**: WebRTC microphone capture permission delegation in `WKWebView` (`requestMediaCapturePermissionFor`) requires **iOS 15.0+**. On iOS 14, text messaging and all other widget features work normally, but voice calls are unavailable.
- **Distribution**: A dedicated mirror repository and semantic versions will be provided later.
