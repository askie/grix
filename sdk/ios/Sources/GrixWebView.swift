#if canImport(UIKit)
import AVFoundation
import UIKit
import WebKit

/// Weak wrapper around `WKScriptMessageHandler` to prevent retain cycles with `WKUserContentController`.
final class GrixWeakScriptMessageHandler: NSObject, WKScriptMessageHandler {
    weak var delegate: WKScriptMessageHandler?

    init(delegate: WKScriptMessageHandler) {
        self.delegate = delegate
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        delegate?.userContentController(userContentController, didReceive: message)
    }
}

/// Embeddable `UIView` hosting the Grix Widget `WKWebView`.
public final class GrixWidgetView: UIView, WKScriptMessageHandler, WKNavigationDelegate, WKUIDelegate {
    public static let bridgeChannelName = "grix"

    private let config: GrixConfig
    private let visitorStore: GrixVisitorStore
    private let onEvent: (GrixBridgeEvent) -> Void
    private var webView: WKWebView!
    private var appURL: URL?
    private var isViewVisible = false

    public init(
        config: GrixConfig,
        visitorStore: GrixVisitorStore,
        onEvent: @escaping (GrixBridgeEvent) -> Void
    ) {
        self.config = config
        self.visitorStore = visitorStore
        self.onEvent = onEvent
        super.init(frame: .zero)
        setupWebView()
        observeApplicationLifecycle()
        loadWidget()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    deinit {
        NotificationCenter.default.removeObserver(self)
        webView?.configuration.userContentController.removeScriptMessageHandler(forName: Self.bridgeChannelName)
    }

    private func setupWebView() {
        let userContentController = WKUserContentController()
        userContentController.add(
            GrixWeakScriptMessageHandler(delegate: self),
            name: Self.bridgeChannelName
        )

        let wkConfig = WKWebViewConfiguration()
        wkConfig.userContentController = userContentController
        wkConfig.allowsInlineMediaPlayback = true
        if #available(iOS 10.0, *) {
            wkConfig.mediaTypesRequiringUserActionForPlayback = []
        }

        let wv = WKWebView(frame: bounds, configuration: wkConfig)
        wv.translatesAutoresizingMaskIntoConstraints = false
        wv.navigationDelegate = self
        wv.uiDelegate = self
        wv.backgroundColor = .systemBackground
        wv.scrollView.bounces = false
        addSubview(wv)

        NSLayoutConstraint.activate([
            wv.topAnchor.constraint(equalTo: topAnchor),
            wv.bottomAnchor.constraint(equalTo: bottomAnchor),
            wv.leadingAnchor.constraint(equalTo: leadingAnchor),
            wv.trailingAnchor.constraint(equalTo: trailingAnchor),
        ])

        self.webView = wv
    }

    private func observeApplicationLifecycle() {
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(handleWillEnterForeground),
            name: UIApplication.willEnterForegroundNotification,
            object: nil
        )
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(handleDidEnterBackground),
            name: UIApplication.didEnterBackgroundNotification,
            object: nil
        )
    }

    public override func didMoveToWindow() {
        super.didMoveToWindow()
        let visibleNow = (window != nil && !isHidden)
        isViewVisible = visibleNow
        notifyWebVisibility(visibleNow)
    }

    public func loadWidget() {
        let existingVisitorKey = visitorStore.visitorKey(for: config.siteKey)
        guard let url = GrixURLBuilder.buildAppURL(config: config, visitorKey: existingVisitorKey) else {
            onEvent(.error(code: "invalid_config", message: "Failed to build widget URL"))
            return
        }
        self.appURL = url
        webView.load(URLRequest(url: url))
    }

    /// Invokes `window.grixWidget.setVisible(bool)` inside the hosted page.
    public func notifyWebVisibility(_ visible: Bool) {
        isViewVisible = visible
        let jsLiteral = visible ? "true" : "false"
        let script = """
        (function(v) {
            if (window.grixWidget && typeof window.grixWidget.setVisible === 'function') {
                window.grixWidget.setVisible(v);
            } else {
                setTimeout(function() {
                    if (window.grixWidget && typeof window.grixWidget.setVisible === 'function') {
                        window.grixWidget.setVisible(v);
                    }
                }, 150);
            }
        })(\(jsLiteral));
        """
        webView?.evaluateJavaScript(script, completionHandler: nil)
    }

    /// Invokes `window.grixWidget.close()` inside the hosted page.
    public func requestClose() {
        let script = "window.grixWidget && typeof window.grixWidget.close === 'function' && window.grixWidget.close();"
        webView?.evaluateJavaScript(script, completionHandler: nil)
    }

    @objc private func handleWillEnterForeground() {
        if window != nil && !isHidden {
            isViewVisible = true
            notifyWebVisibility(true)
        }
    }

    @objc private func handleDidEnterBackground() {
        if isViewVisible {
            notifyWebVisibility(false)
        }
    }

    public func evaluateJavaScript(_ script: String, completionHandler: ((Any?, Error?) -> Void)? = nil) {
        webView?.evaluateJavaScript(script, completionHandler: completionHandler)
    }

    // MARK: - WKScriptMessageHandler

    public func userContentController(
        _ userContentController: WKUserContentController,
        didReceive message: WKScriptMessage
    ) {
        guard message.name == Self.bridgeChannelName,
              let event = GrixBridgeEvent.parse(message.body)
        else {
            return
        }

        if case let .ready(visitorKey, _) = event, !visitorKey.isEmpty {
            visitorStore.saveVisitorKey(visitorKey, for: config.siteKey)
        }

        onEvent(event)
    }

    // MARK: - WKNavigationDelegate

    public func webView(
        _ webView: WKWebView,
        decidePolicyFor navigationAction: WKNavigationAction,
        decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
    ) {
        guard let targetURL = navigationAction.request.url else {
            decisionHandler(.cancel)
            return
        }

        // Only the configured API origin may stay inside the WebView. This
        // includes JS-initiated top-level navigations, not just link taps.
        if isAPISource(
            scheme: targetURL.scheme,
            host: targetURL.host,
            port: targetURL.port
        ) {
            decisionHandler(.allow)
            return
        }

        let scheme = targetURL.scheme?.lowercased() ?? ""
        if scheme == "http" || scheme == "https" || UIApplication.shared.canOpenURL(targetURL) {
            UIApplication.shared.open(targetURL, options: [:], completionHandler: nil)
        }
        decisionHandler(.cancel)
    }

    public func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        if window != nil && !isHidden {
            isViewVisible = true
            notifyWebVisibility(true)
        }
    }

    // MARK: - WKUIDelegate (Microphone permission for voice calls on iOS 15+)

    public func webView(
        _ webView: WKWebView,
        createWebViewWith configuration: WKWebViewConfiguration,
        for navigationAction: WKNavigationAction,
        windowFeatures: WKWindowFeatures
    ) -> WKWebView? {
        if let url = navigationAction.request.url,
           let scheme = url.scheme?.lowercased(),
           scheme == "http" || scheme == "https"
        {
            UIApplication.shared.open(url, options: [:], completionHandler: nil)
        }
        return nil
    }

    @available(iOS 15.0, *)
    public func webView(
        _ webView: WKWebView,
        requestMediaCapturePermissionFor origin: WKSecurityOrigin,
        initiatedByFrame frame: WKFrameInfo,
        type: WKMediaCaptureType,
        decisionHandler: @escaping (WKPermissionDecision) -> Void
    ) {
        guard isAPISource(
            scheme: origin.protocol,
            host: origin.host,
            port: origin.port
        ) else {
            decisionHandler(.deny)
            return
        }

        NotificationCenter.default.post(
            name: Notification.Name("GrixWidgetDidRequestMediaCapturePermission"),
            object: self,
            userInfo: ["host": origin.host, "port": origin.port]
        )
        AVAudioSession.sharedInstance().requestRecordPermission { granted in
            DispatchQueue.main.async {
                decisionHandler(granted ? .grant : .deny)
            }
        }
    }

    /// Returns true when the URL origin matches `apiBase` exactly.
    private func isAPISource(scheme: String?, host: String?, port: Int?) -> Bool {
        guard let baseURL = URL(string: config.apiBase),
              let baseScheme = baseURL.scheme?.lowercased(),
              let baseHost = baseURL.host?.lowercased()
        else {
            return false
        }
        let candidateScheme = scheme?.lowercased() ?? ""
        let candidateHost = host?.lowercased() ?? ""
        return candidateScheme == baseScheme &&
            candidateHost == baseHost &&
            effectivePort(scheme: candidateScheme, port: port) ==
            effectivePort(scheme: baseScheme, port: baseURL.port)
    }

    private func effectivePort(scheme: String, port: Int?) -> Int? {
        if let port = port {
            return port
        }
        switch scheme {
        case "https": return 443
        case "http": return 80
        default: return nil
        }
    }
}

/// Embeddable or modal `UIViewController` wrapping `GrixWidgetView`.
public final class GrixWidgetViewController: UIViewController {
    private let config: GrixConfig
    private let visitorStore: GrixVisitorStore
    private let onEvent: (GrixBridgeEvent) -> Void
    private var widgetView: GrixWidgetView?

    public init(
        config: GrixConfig,
        visitorStore: GrixVisitorStore,
        onEvent: @escaping (GrixBridgeEvent) -> Void
    ) {
        self.config = config
        self.visitorStore = visitorStore
        self.onEvent = onEvent
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    public override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground

        let wv = GrixWidgetView(
            config: config,
            visitorStore: visitorStore,
            onEvent: { [weak self] event in
                if case .close = event {
                    self?.dismissIfModal()
                }
                self?.onEvent(event)
            }
        )
        wv.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(wv)

        NSLayoutConstraint.activate([
            wv.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            wv.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            wv.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            wv.trailingAnchor.constraint(equalTo: view.trailingAnchor),
        ])
        self.widgetView = wv
    }

    public override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        widgetView?.notifyWebVisibility(true)
    }

    public override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        widgetView?.notifyWebVisibility(false)
    }

    private func dismissIfModal() {
        if presentingViewController != nil {
            dismiss(animated: true)
        }
    }
}
#endif
