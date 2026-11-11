import GrixWidget
import UIKit

final class DemoViewController: UIViewController {
    private let siteKeyField = UITextField()
    private let apiBaseField = UITextField()
    private let appIdOverrideField = UITextField()
    private let statusLabel = UILabel()
    private let embedContainer = UIView()
    private var embeddedView: UIView?
    private var launcherVisible = false

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Grix Widget iOS Demo"
        view.backgroundColor = .systemGroupedBackground

        let env = ProcessInfo.processInfo.environment
        let initialSiteKey = env["GRIX_SITE_KEY"] ?? "demo_site_key"
        let initialApiBase = env["GRIX_API_BASE"] ?? "http://127.0.0.1:27180"
        let initialAppIdOverride = env["GRIX_APP_ID_OVERRIDE"] ?? ""
        let autoAction = env["GRIX_AUTO_ACTION"] ?? ""

        setupUI(siteKey: initialSiteKey, apiBase: initialApiBase, appIdOverride: initialAppIdOverride)
        applyConfiguration()

        NotificationCenter.default.addObserver(
            forName: Notification.Name("GrixWidgetDidRequestMediaCapturePermission"),
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.appendLog("requestMediaCapturePermissionFor triggered")
        }
        NotificationCenter.default.addObserver(
            forName: Notification.Name("GrixWidgetDidOpenExternalURL"),
            object: nil,
            queue: .main
        ) { [weak self] note in
            let urlStr = note.userInfo?["url"] as? String ?? ""
            self?.appendLog("open_url(\(urlStr))")
        }

        if autoAction == "present" {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) { [weak self] in
                self?.handlePresentTapped()
            }
        } else if autoAction == "embed" {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self] in
                self?.handleEmbedTapped()
            }
        } else if autoAction == "embed_full_test" {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self] in
                self?.runAutomatedAcceptanceSequence()
            }
        }
    }

    private func runAutomatedAcceptanceSequence() {
        handleEmbedTapped()
        guard let grixView = embeddedView as? GrixWidgetView else { return }

        // 1. After 2.5s, send a visitor message via the hosted page composer
        DispatchQueue.main.asyncAfter(deadline: .now() + 2.5) { [weak self, weak grixView] in
            self?.appendLog("auto: sending visitor message")
            grixView?.evaluateJavaScript("""
                (function() {
                    var input = document.getElementById('input');
                    var send = document.getElementById('send');
                    if (input && send) {
                        input.value = '你好，我是来自 iOS App 的测试访客！';
                        send.click();
                    }
                })();
            """)
        }

        // 2. After 4.5s, setVisible(false) so subsequent owner message increments unread badge
        DispatchQueue.main.asyncAfter(deadline: .now() + 4.5) { [weak self, weak grixView] in
            self?.appendLog("auto: setVisible(false) + show launcher")
            self?.launcherVisible = true
            GrixWidget.setLauncherVisible(true)
            grixView?.notifyWebVisibility(false)
        }

        // 3. After 7.5s, setVisible(true) so unread badge clears to 0
        DispatchQueue.main.asyncAfter(deadline: .now() + 7.5) { [weak self, weak grixView] in
            self?.appendLog("auto: setVisible(true) to clear unread")
            grixView?.notifyWebVisibility(true)
        }

        // 4. After 9.5s, click the markdown link inside the peer message to trigger open_url
        DispatchQueue.main.asyncAfter(deadline: .now() + 9.5) { [weak self, weak grixView] in
            self?.appendLog("auto: clicking link in message")
            grixView?.evaluateJavaScript("""
                (function() {
                    var link = document.querySelector('#msgs a[href]');
                    if (link) { link.click(); }
                })();
            """)
        }

        // 5. After 11.5s, click the voice call button 📞 to trigger microphone permission request
        DispatchQueue.main.asyncAfter(deadline: .now() + 11.5) { [weak self, weak grixView] in
            self?.appendLog("auto: clicking callBtn for voice call")
            grixView?.evaluateJavaScript("""
                (function() {
                    var btn = document.getElementById('callBtn');
                    if (btn) { btn.click(); }
                })();
            """)
        }

        // 6. After 14.5s, invoke close button to trigger onClose
        DispatchQueue.main.asyncAfter(deadline: .now() + 14.5) { [weak self, weak grixView] in
            self?.appendLog("auto: invoking requestClose()")
            grixView?.requestClose()
        }
    }

    private func setupUI(siteKey: String, apiBase: String, appIdOverride: String) {
        let stack = UIStackView()
        stack.axis = .vertical
        stack.spacing = 10
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        configureTextField(siteKeyField, placeholder: "Site Key", text: siteKey)
        configureTextField(apiBaseField, placeholder: "API Base URL", text: apiBase)
        configureTextField(appIdOverrideField, placeholder: "App ID Override (blank = Bundle ID)", text: appIdOverride)

        let btnRow1 = UIStackView()
        btnRow1.axis = .horizontal
        btnRow1.spacing = 8
        btnRow1.distribution = .fillEqually

        let presentBtn = makeButton(title: "present()", color: .systemTeal, action: #selector(handlePresentTapped))
        let embedBtn = makeButton(title: "makeView()", color: .systemBlue, action: #selector(handleEmbedTapped))
        let launcherBtn = makeButton(title: "Toggle Launcher", color: .systemIndigo, action: #selector(handleLauncherTapped))
        btnRow1.addArrangedSubview(presentBtn)
        btnRow1.addArrangedSubview(embedBtn)
        btnRow1.addArrangedSubview(launcherBtn)

        statusLabel.numberOfLines = 0
        statusLabel.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        statusLabel.textColor = .secondaryLabel
        statusLabel.text = "Status: Ready (Bundle ID: \(Bundle.main.bundleIdentifier ?? "-"))"

        embedContainer.backgroundColor = .secondarySystemGroupedBackground
        embedContainer.layer.cornerRadius = 12
        embedContainer.layer.masksToBounds = true
        embedContainer.translatesAutoresizingMaskIntoConstraints = false

        stack.addArrangedSubview(siteKeyField)
        stack.addArrangedSubview(apiBaseField)
        stack.addArrangedSubview(appIdOverrideField)
        stack.addArrangedSubview(btnRow1)
        stack.addArrangedSubview(statusLabel)
        stack.addArrangedSubview(embedContainer)

        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 12),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            stack.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor, constant: -12),
            embedContainer.heightAnchor.constraint(greaterThanOrEqualToConstant: 320),
        ])
    }

    private func configureTextField(_ tf: UITextField, placeholder: String, text: String) {
        tf.placeholder = placeholder
        tf.text = text
        tf.borderStyle = .roundedRect
        tf.font = .systemFont(ofSize: 13)
        tf.autocapitalizationType = .none
        tf.autocorrectionType = .no
    }

    private func makeButton(title: String, color: UIColor, action: Selector) -> UIButton {
        let btn = UIButton(type: .system)
        btn.setTitle(title, for: .normal)
        btn.setTitleColor(.white, for: .normal)
        btn.titleLabel?.font = .systemFont(ofSize: 13, weight: .semibold)
        btn.backgroundColor = color
        btn.layer.cornerRadius = 8
        btn.heightAnchor.constraint(equalToConstant: 38).isActive = true
        btn.addTarget(self, action: action, for: .touchUpInside)
        return btn
    }

    private func applyConfiguration() {
        let overrideText = appIdOverrideField.text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        GrixWidget.configure(
            siteKey: siteKeyField.text ?? "",
            apiBase: apiBaseField.text ?? "http://127.0.0.1:27180",
            locale: "zh-CN",
            themeColor: "#0f766e",
            title: "Grix iOS 客服",
            user: GrixUser(name: "iOS 访客", email: "ios@example.com"),
            appIdOverride: overrideText.isEmpty ? nil : overrideText
        )

        GrixWidget.onClose = { [weak self] in
            self?.appendLog("onClose triggered")
        }
        GrixWidget.onUnreadChange = { [weak self] count in
            self?.appendLog("onUnreadChange(\(count))")
        }
        GrixWidget.onSessionClosed = { [weak self] reason in
            self?.appendLog("onSessionClosed(\(reason))")
        }
        GrixWidget.onError = { [weak self] code, message in
            self?.appendLog("onError(\(code)): \(message)")
        }
    }

    private func appendLog(_ text: String) {
        print("[GrixExample] \(text)")
        if let testCallbackURL = ProcessInfo.processInfo.environment["GRIX_TEST_CALLBACK_URL"],
           let url = URL(string: testCallbackURL)
        {
            var req = URLRequest(url: url)
            req.httpMethod = "POST"
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            let body: [String: Any] = [
                "event": text,
                "visitor_key": GrixWidget.shared.visitorStore.visitorKey(for: siteKeyField.text ?? "") ?? "",
            ]
            req.httpBody = try? JSONSerialization.data(withJSONObject: body)
            URLSession.shared.dataTask(with: req).resume()
        }
        DispatchQueue.main.async { [weak self] in
            self?.statusLabel.text = "Last Event: \(text)"
        }
    }

    @objc private func handlePresentTapped() {
        applyConfiguration()
        GrixWidget.present(from: self)
    }

    @objc private func handleEmbedTapped() {
        applyConfiguration()
        embeddedView?.removeFromSuperview()
        let v = GrixWidget.makeView()
        v.translatesAutoresizingMaskIntoConstraints = false
        embedContainer.addSubview(v)
        NSLayoutConstraint.activate([
            v.topAnchor.constraint(equalTo: embedContainer.topAnchor),
            v.bottomAnchor.constraint(equalTo: embedContainer.bottomAnchor),
            v.leadingAnchor.constraint(equalTo: embedContainer.leadingAnchor),
            v.trailingAnchor.constraint(equalTo: embedContainer.trailingAnchor),
        ])
        embeddedView = v
        appendLog("Embedded makeView() loaded")
    }

    @objc private func handleLauncherTapped() {
        applyConfiguration()
        launcherVisible.toggle()
        GrixWidget.setLauncherVisible(launcherVisible)
        appendLog("setLauncherVisible(\(launcherVisible))")
    }
}
