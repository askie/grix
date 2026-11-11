import Foundation
#if canImport(UIKit)
import UIKit
#endif

/// Unified entry point for the Grix Website Widget iOS SDK.
public final class GrixWidget {
    public static let shared = GrixWidget()

    /// Fired when the visitor taps the close button or when `close` event is emitted.
    public static var onClose: (() -> Void)? {
        get { shared.onCloseHandler }
        set { shared.onCloseHandler = newValue }
    }

    /// Fired when the unread message count changes.
    public static var onUnreadChange: ((Int) -> Void)? {
        get { shared.onUnreadChangeHandler }
        set { shared.onUnreadChangeHandler = newValue }
    }

    /// Fired when the session is closed by the agent/owner (`reason` e.g. `"closed"`).
    public static var onSessionClosed: ((String) -> Void)? {
        get { shared.onSessionClosedHandler }
        set { shared.onSessionClosedHandler = newValue }
    }

    /// Fired when an error occurs (e.g. `app_not_allowed`, `site_disabled`, `network_error`).
    public static var onError: ((String, String) -> Void)? {
        get { shared.onErrorHandler }
        set { shared.onErrorHandler = newValue }
    }

    public private(set) var config: GrixConfig?
    public var visitorStore: GrixVisitorStore = GrixKeychainStore()

    private var onCloseHandler: (() -> Void)?
    private var onUnreadChangeHandler: ((Int) -> Void)?
    private var onSessionClosedHandler: ((String) -> Void)?
    private var onErrorHandler: ((String, String) -> Void)?
    private var unreadCount: Int = 0

    #if canImport(UIKit)
    private var launcherButton: GrixLauncherButton?
    private weak var modalViewController: GrixWidgetViewController?
    #endif

    private init() {}

    /// Configures the Grix Widget SDK.
    public static func configure(
        siteKey: String,
        apiBase: String = GrixConfig.defaultApiBase,
        locale: String? = nil,
        themeColor: String? = nil,
        title: String? = nil,
        user: GrixUser? = nil,
        appIdOverride: String? = nil
    ) {
        shared.config = GrixConfig(
            siteKey: siteKey,
            apiBase: apiBase,
            locale: locale,
            themeColor: themeColor,
            title: title,
            user: user,
            appIdOverride: appIdOverride
        )
        #if canImport(UIKit)
        DispatchQueue.main.async {
            shared.launcherButton?.updateThemeColor(themeColor)
        }
        #endif
    }

    internal func handleBridgeEvent(_ event: GrixBridgeEvent) {
        switch event {
        case .ready:
            break
        case .close:
            onCloseHandler?()
        case let .sessionClosed(reason):
            onSessionClosedHandler?(reason)
        case let .unread(count):
            unreadCount = count
            #if canImport(UIKit)
            DispatchQueue.main.async { [weak self] in
                self?.launcherButton?.updateUnreadCount(count)
            }
            #endif
            onUnreadChangeHandler?(count)
        case let .openURL(url):
            #if canImport(UIKit)
            DispatchQueue.main.async {
                NotificationCenter.default.post(
                    name: Notification.Name("GrixWidgetDidOpenExternalURL"),
                    object: self,
                    userInfo: ["url": url.absoluteString]
                )
                UIApplication.shared.open(url, options: [:], completionHandler: nil)
            }
            #endif
        case let .error(code, message):
            onErrorHandler?(code, message)
        }
    }

    #if canImport(UIKit)
    /// Presents the Grix chat interface modally from the top-most view controller (or `presentingController` if supplied).
    public static func present(from presentingController: UIViewController? = nil, animated: Bool = true) {
        guard let config = shared.config else {
            shared.onErrorHandler?("not_configured", "GrixWidget.configure(...) must be called before present().")
            return
        }
        guard let presenter = presentingController ?? shared.topViewController() else {
            shared.onErrorHandler?("no_presenter", "Unable to locate a root UIViewController to present GrixWidget.")
            return
        }
        if let existing = shared.modalViewController, existing.presentingViewController != nil {
            return
        }
        let vc = GrixWidgetViewController(
            config: config,
            visitorStore: shared.visitorStore,
            onEvent: { event in
                shared.handleBridgeEvent(event)
            }
        )
        vc.modalPresentationStyle = .pageSheet
        shared.modalViewController = vc
        presenter.present(vc, animated: animated)
    }

    /// Creates an embeddable `UIView` containing the Grix Widget.
    public static func makeView() -> UIView {
        let cfg = shared.config ?? GrixConfig(siteKey: "")
        if shared.config == nil {
            DispatchQueue.main.async {
                shared.onErrorHandler?("not_configured", "GrixWidget.configure(...) must be called before makeView().")
            }
        }
        return GrixWidgetView(
            config: cfg,
            visitorStore: shared.visitorStore,
            onEvent: { event in
                shared.handleBridgeEvent(event)
            }
        )
    }

    /// Creates an embeddable `UIViewController` containing the Grix Widget.
    public static func makeViewController() -> UIViewController {
        let cfg = shared.config ?? GrixConfig(siteKey: "")
        if shared.config == nil {
            DispatchQueue.main.async {
                shared.onErrorHandler?("not_configured", "GrixWidget.configure(...) must be called before makeViewController().")
            }
        }
        return GrixWidgetViewController(
            config: cfg,
            visitorStore: shared.visitorStore,
            onEvent: { event in
                shared.handleBridgeEvent(event)
            }
        )
    }

    /// Shows or hides the floating launcher button on the active window.
    public static func setLauncherVisible(_ visible: Bool) {
        DispatchQueue.main.async {
            shared.updateLauncherVisibility(visible)
        }
    }

    private func updateLauncherVisibility(_ visible: Bool) {
        if !visible {
            launcherButton?.removeFromSuperview()
            launcherButton = nil
            return
        }

        guard let window = activeKeyWindow() else {
            return
        }

        if let button = launcherButton {
            if button.superview !== window {
                button.removeFromSuperview()
                attachLauncher(button, to: window)
            }
            button.updateThemeColor(config?.themeColor)
            button.updateUnreadCount(unreadCount)
            button.isHidden = false
            return
        }

        let button = GrixLauncherButton(frame: .zero)
        button.translatesAutoresizingMaskIntoConstraints = false
        button.updateThemeColor(config?.themeColor)
        button.updateUnreadCount(unreadCount)
        button.addTarget(self, action: #selector(handleLauncherTapped), for: .touchUpInside)
        attachLauncher(button, to: window)
        launcherButton = button
    }

    private func attachLauncher(_ button: GrixLauncherButton, to window: UIWindow) {
        window.addSubview(button)
        NSLayoutConstraint.activate([
            button.widthAnchor.constraint(equalToConstant: 56),
            button.heightAnchor.constraint(equalToConstant: 56),
            button.trailingAnchor.constraint(equalTo: window.safeAreaLayoutGuide.trailingAnchor, constant: -20),
            button.bottomAnchor.constraint(equalTo: window.safeAreaLayoutGuide.bottomAnchor, constant: -24),
        ])
    }

    @objc private func handleLauncherTapped() {
        GrixWidget.present()
    }

    private func activeKeyWindow() -> UIWindow? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        for scene in scenes where scene.activationState == .foregroundActive {
            if let keyWin = scene.windows.first(where: { $0.isKeyWindow }) ?? scene.windows.first {
                return keyWin
            }
        }
        return scenes.flatMap { $0.windows }.first
    }

    private func topViewController(base: UIViewController? = nil) -> UIViewController? {
        let root = base ?? activeKeyWindow()?.rootViewController
        if let nav = root as? UINavigationController {
            return topViewController(base: nav.visibleViewController)
        }
        if let tab = root as? UITabBarController, let selected = tab.selectedViewController {
            return topViewController(base: selected)
        }
        if let presented = root?.presentedViewController {
            return topViewController(base: presented)
        }
        return root
    }
    #endif
}
