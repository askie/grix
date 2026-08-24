import AVFoundation
import AuthenticationServices
import Flutter
import GoogleSignIn
import Photos
import UIKit
import UserNotifications

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  private let apnsEnvironmentInfoKey = "AIBOT_APNS_ENVIRONMENT"
  private let mermaidImageSaverChannel = "pub.dhf.grix/mermaid_image_saver"
  private let pushRegistrationChannel = "pub.dhf.grix/push_registration"
  private let googleSignInChannel = "pub.dhf.grix/google_sign_in"
  private let appleSignInChannel = "pub.dhf.grix/apple_sign_in"
  private let appBadgeChannel = "pub.dhf.grix/app_badge"
  private let pushFilterChannel = "pub.dhf.grix/push_filter"
  private let pushTapChannel = "pub.dhf.grix/push_tap"
  private let audioSessionChannel = "pub.dhf.grix/audio_session"
  private let nativeClipboardChannel = "pub.dhf.grix/native_clipboard"
  private let notifyActionChannel = "pub.dhf.grix/notify_action"
  private var apnsDeviceTokenHex = ""
  private var pendingPushResult: FlutterResult?
  private var activeSessionID: String? = nil
  private var pendingTapPayload: [String: String]? = nil
  private var pushTapMethodChannel: FlutterMethodChannel?
  // 语音通话：保存音频会话 channel，用于原生→Flutter 回调（系统中断结束等）。
  private var audioSessionMethodChannel: FlutterMethodChannel?
  // Apple Sign-In: ASAuthorizationController.delegate 是 weak，必须持有强引用防止 ARC 释放。
  private var appleSignInDelegate: AppleSignInDelegate?

  // 离线通知回调（approve/deny/stop/reply）：用后台 URLSession，App 被 kill 也能完成。
  private let notifyCallbackURLKey = "grix_notify_callback_url"
  private let notifyBgSessionIdentifier = "pub.dhf.grix.notify-callback"
  private var notifyBgCompletionHandler: (() -> Void)?
  private lazy var notifyBgSession: URLSession = {
    let config = URLSessionConfiguration.background(withIdentifier: notifyBgSessionIdentifier)
    config.isDiscretionary = false
    config.sessionSendsLaunchEvents = true
    return URLSession(configuration: config, delegate: self, delegateQueue: nil)
  }()
  private static let notifyActionIdentifiers: Set<String> = ["approve", "deny", "stop", "reply"]

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    NativeSentryEventDeduplicator.install()
    UNUserNotificationCenter.current().delegate = self
    registerNotificationCategories()
    TextInputFeatureSwizzler.shared.apply()
    UIApplication.shared.applicationSupportsShakeToEdit = false

    if let remoteNotification = launchOptions?[.remoteNotification] as? [String: Any],
       let sessionId = stringifyPushField(remoteNotification["session_id"]),
       !sessionId.isEmpty {
      let messageId = stringifyPushField(remoteNotification["message_id"]) ?? ""
      let recipientId = stringifyPushField(remoteNotification["recipient_id"]) ?? ""
      NSLog("[PushTap] cold start: stored pending tap session_id=%@ message_id=%@", sessionId, messageId)
      pendingTapPayload = [
        "session_id": sessionId,
        "message_id": messageId,
        "recipient_id": recipientId,
      ]
    }

    NotificationCenter.default.addObserver(
      self,
      selector: #selector(handleAudioSessionInterruption(_:)),
      name: AVAudioSession.interruptionNotification,
      object: nil
    )
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  // MARK: - FlutterImplicitEngineDelegate (UIScene lifecycle)

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    NSLog("[Grix] didInitializeImplicitFlutterEngine called")
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    NSLog("[Grix] plugins registered")
    guard let registrar = engineBridge.pluginRegistry.registrar(forPlugin: "AppDelegate") else {
      NSLog("[Grix] ERROR: registrar for AppDelegate is nil")
      return
    }
    setupMethodChannels(messenger: registrar.messenger())
    NSLog("[Grix] method channels setup complete")
  }

  private func setupMethodChannels(messenger: FlutterBinaryMessenger) {
    TextDocumentBridge.shared.configure(messenger: messenger)
    let imageChannel = FlutterMethodChannel(
      name: mermaidImageSaverChannel,
      binaryMessenger: messenger
    )
    imageChannel.setMethodCallHandler { [weak self] call, result in
      switch call.method {
      case "saveImageToGallery":
        guard
          let args = call.arguments as? [String: Any],
          let bytes = args["bytes"] as? FlutterStandardTypedData,
          let fileName = args["fileName"] as? String,
          !fileName.isEmpty
        else {
          result(
            FlutterError(
              code: "invalid_args",
              message: "Missing bytes or fileName",
              details: nil
            )
          )
          return
        }
        self?.saveImageToPhotoLibrary(
          bytes: bytes.data,
          fileName: fileName,
          result: result
        )
      case "saveVideoToGallery":
        guard
          let args = call.arguments as? [String: Any],
          let filePath = args["filePath"] as? String,
          !filePath.isEmpty
        else {
          result(
            FlutterError(
              code: "invalid_args",
              message: "Missing filePath",
              details: nil
            )
          )
          return
        }
        self?.saveVideoToPhotoLibrary(filePath: filePath, result: result)
      default:
        result(FlutterMethodNotImplemented)
      }
    }

    let pushChannel = FlutterMethodChannel(
      name: pushRegistrationChannel,
      binaryMessenger: messenger
    )
    pushChannel.setMethodCallHandler { [weak self] call, result in
      guard call.method == "registerApplePush" else {
        result(FlutterMethodNotImplemented)
        return
      }
      self?.registerApplePush(result: result)
    }

    let googleChannel = FlutterMethodChannel(
      name: googleSignInChannel,
      binaryMessenger: messenger
    )
    googleChannel.setMethodCallHandler { [weak self] call, result in
      guard call.method == "signInWithGoogle" else {
        result(FlutterMethodNotImplemented)
        return
      }
      self?.signInWithGoogle(result: result)
    }

    let badgeChannel = FlutterMethodChannel(
      name: appBadgeChannel,
      binaryMessenger: messenger
    )
    badgeChannel.setMethodCallHandler { [weak self] call, result in
      guard call.method == "setAppBadge" else {
        result(FlutterMethodNotImplemented)
        return
      }
      self?.setAppBadge(call: call, result: result)
    }

    let filterChannel = FlutterMethodChannel(
      name: pushFilterChannel,
      binaryMessenger: messenger
    )
    filterChannel.setMethodCallHandler { [weak self] call, result in
      guard call.method == "setActiveSessionID",
            let args = call.arguments as? [String: Any]
      else {
        result(FlutterMethodNotImplemented)
        return
      }
      let sid = (args["sessionId"] as? String)?
        .trimmingCharacters(in: .whitespaces) ?? ""
      self?.activeSessionID = sid.isEmpty ? nil : sid
      result(nil)
    }

    let appleChannel = FlutterMethodChannel(
      name: appleSignInChannel,
      binaryMessenger: messenger
    )
    appleChannel.setMethodCallHandler { [weak self] call, result in
      guard call.method == "signInWithApple" else {
        result(FlutterMethodNotImplemented)
        return
      }
      self?.signInWithApple(result: result)
    }

    let tapChannel = FlutterMethodChannel(
      name: pushTapChannel,
      binaryMessenger: messenger
    )
    pushTapMethodChannel = tapChannel
    tapChannel.setMethodCallHandler { _, result in
      result(FlutterMethodNotImplemented)
    }

    let notifyChannel = FlutterMethodChannel(
      name: notifyActionChannel,
      binaryMessenger: messenger
    )
    notifyChannel.setMethodCallHandler { [weak self] call, result in
      guard call.method == "setCallbackUrl",
            let args = call.arguments as? [String: Any],
            let url = (args["url"] as? String)?
              .trimmingCharacters(in: .whitespacesAndNewlines),
            !url.isEmpty
      else {
        result(FlutterMethodNotImplemented)
        return
      }
      UserDefaults.standard.set(url, forKey: self?.notifyCallbackURLKey ?? "grix_notify_callback_url")
      result(nil)
    }

    let audioChannel = FlutterMethodChannel(
      name: audioSessionChannel,
      binaryMessenger: messenger
    )
    self.audioSessionMethodChannel = audioChannel
    audioChannel.setMethodCallHandler { call, result in
      guard call.method == "releaseAudioSession" else {
        result(FlutterMethodNotImplemented)
        return
      }
      self.releaseAudioSession(result: result)
    }

    let clipboardChannel = FlutterMethodChannel(
      name: nativeClipboardChannel,
      binaryMessenger: messenger
    )
    clipboardChannel.setMethodCallHandler { call, result in
      switch call.method {
      case "getChangeCount":
        result(UIPasteboard.general.changeCount)
      case "hasStrings":
        if #available(iOS 16.0, *) {
          result(UIPasteboard.general.hasStrings)
        } else {
          result(UIPasteboard.general.strings?.isEmpty == false)
        }
      default:
        result(FlutterMethodNotImplemented)
      }
    }

    if let pendingTap = pendingTapPayload,
       let tapSid = pendingTap["session_id"],
       !tapSid.isEmpty {
      pendingTapPayload = nil
      let tapMid = pendingTap["message_id"] ?? ""
      let tapRid = pendingTap["recipient_id"] ?? ""
      NSLog("[PushTap] scheduling delayed tap for session_id=%@ message_id=%@", tapSid, tapMid)
      DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { [weak self] in
        NSLog("[PushTap] firing delayed tap for session_id=%@ message_id=%@", tapSid, tapMid)
        self?.notifyPushTap(sessionId: tapSid, messageId: tapMid, recipientId: tapRid)
      }
    }
  }

  @objc private func handleAudioSessionInterruption(_ notification: Notification) {
    guard
      let info = notification.userInfo,
      let typeValue = info[AVAudioSessionInterruptionTypeKey] as? UInt,
      let type = AVAudioSession.InterruptionType(rawValue: typeValue)
    else { return }
    // 仅在中断结束（来电挂断/闹钟停/其他 App 释放音频）时通知 Flutter 侧。
    if type == .ended {
      DispatchQueue.main.async { [weak self] in
        self?.audioSessionMethodChannel?.invokeMethod(
          "onAudioInterruptionEnded",
          arguments: nil
        )
      }
    }
  }

  override func application(
    _ app: UIApplication,
    open url: URL,
    options: [UIApplication.OpenURLOptionsKey: Any] = [:]
  ) -> Bool {
    if GIDSignIn.sharedInstance.handle(url) {
      return true
    }
    return super.application(app, open: url, options: options)
  }

  override func application(
    _ application: UIApplication,
    didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
  ) {
    super.application(application, didRegisterForRemoteNotificationsWithDeviceToken: deviceToken)
    apnsDeviceTokenHex = deviceToken.map { String(format: "%02x", $0) }.joined()
    if let pendingPushResult {
      guard let pushEnv = currentAPNsPushEnv() else {
        pendingPushResult(
          FlutterError(
            code: "apns_env_missing",
            message: "Missing valid APNs environment in Info.plist",
            details: nil
          )
        )
        self.pendingPushResult = nil
        return
      }
      pendingPushResult(pushRegistrationPayload(pushEnv: pushEnv))
      self.pendingPushResult = nil
    }
  }

  override func application(
    _ application: UIApplication,
    didFailToRegisterForRemoteNotificationsWithError error: Error
  ) {
    super.application(application, didFailToRegisterForRemoteNotificationsWithError: error)
    if let pendingPushResult {
      pendingPushResult(
        FlutterError(
          code: "apns_register_failed",
          message: error.localizedDescription,
          details: nil
        )
      )
      self.pendingPushResult = nil
    }
  }

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    willPresent notification: UNNotification,
    withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
  ) {
    // Suppress banner and sound when the user is actively viewing the session
    // this notification belongs to. Badge is still updated so the count stays correct.
    let userInfo = notification.request.content.userInfo
    if let notifSessionID = userInfo["session_id"] as? String,
       let active = activeSessionID,
       !active.isEmpty,
       notifSessionID == active {
      completionHandler([.badge])
      return
    }
    if #available(iOS 14.0, *) {
      completionHandler([.banner, .sound, .badge])
      return
    }
    completionHandler([.alert, .sound, .badge])
  }

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    didReceive response: UNNotificationResponse,
    withCompletionHandler completionHandler: @escaping () -> Void
  ) {
    let userInfo = response.notification.request.content.userInfo
    let actionID = response.actionIdentifier

    // Offline action buttons (approve/deny/stop/reply): POST to /notify-callback
    // via a background session so the request completes even if the app is killed.
    if AppDelegate.notifyActionIdentifiers.contains(actionID),
       let token = stringifyPushField(userInfo["action_token"]) {
      var text: String? = nil
      if let textResponse = response as? UNTextInputNotificationResponse {
        text = textResponse.userText
      }
      performNotifyCallback(token: token, action: actionID, text: text, completion: completionHandler)
      return
    }

    if let sessionId = stringifyPushField(userInfo["session_id"]), !sessionId.isEmpty {
      let messageId = stringifyPushField(userInfo["message_id"])
      let recipientId = stringifyPushField(userInfo["recipient_id"])
      NSLog("[PushTap] didReceive: session_id=%@ message_id=%@", sessionId, messageId ?? "")
      notifyPushTap(sessionId: sessionId, messageId: messageId, recipientId: recipientId)
    }
    completionHandler()
  }

  /// Localized titles for the notification action buttons, keyed by language
  /// code. Follows the phone's system language (action titles are fixed at
  /// registration time, so they cannot follow the in-app language setting).
  /// Order per language: approve, deny, stop, reply, send, reply placeholder.
  private static let notifyActionCopy: [String: [String]] = [
    "zh": ["批准", "拒绝", "停止任务", "回复", "发送", "输入回复内容..."],
    "en": ["Approve", "Deny", "Stop task", "Reply", "Send", "Type a reply..."],
    "ja": ["承認", "拒否", "タスクを停止", "返信", "送信", "返信を入力..."],
    "ko": ["승인", "거부", "작업 중지", "답장", "보내기", "답장 입력..."],
    "de": ["Genehmigen", "Ablehnen", "Aufgabe stoppen", "Antworten", "Senden", "Antwort eingeben..."],
    "fr": ["Approuver", "Refuser", "Arrêter la tâche", "Répondre", "Envoyer", "Saisir une réponse..."],
    "es": ["Aprobar", "Denegar", "Detener tarea", "Responder", "Enviar", "Escribe una respuesta..."],
    "pt": ["Aprovar", "Negar", "Parar tarefa", "Responder", "Enviar", "Digite uma resposta..."],
    "ru": ["Одобрить", "Отклонить", "Остановить задачу", "Ответить", "Отправить", "Введите ответ..."],
    "ar": ["موافقة", "رفض", "إيقاف المهمة", "رد", "إرسال", "اكتب ردًا..."],
    "hi": ["स्वीकृत करें", "अस्वीकार करें", "कार्य रोकें", "जवाब दें", "भेजें", "जवाब लिखें..."],
  ]

  private static func localizedNotifyActionCopy() -> [String] {
    let preferred = Locale.preferredLanguages.first ?? "zh"
    let lang = String(preferred.prefix(2)).lowercased()
    return notifyActionCopy[lang] ?? notifyActionCopy["zh"]!
  }

  private func registerNotificationCategories() {
    let copy = AppDelegate.localizedNotifyActionCopy()
    let approve = UNNotificationAction(
      identifier: "approve",
      title: copy[0],
      options: [.authenticationRequired]
    )
    let deny = UNNotificationAction(
      identifier: "deny",
      title: copy[1],
      options: [.destructive, .authenticationRequired]
    )
    let stop = UNNotificationAction(
      identifier: "stop",
      title: copy[2],
      options: [.destructive, .authenticationRequired]
    )
    let approvalCategory = UNNotificationCategory(
      identifier: "APPROVAL_REQUEST",
      actions: [approve, deny, stop],
      intentIdentifiers: [],
      options: []
    )

    let reply = UNTextInputNotificationAction(
      identifier: "reply",
      title: copy[3],
      options: [],
      textInputButtonTitle: copy[4],
      textInputPlaceholder: copy[5]
    )
    let questionCategory = UNNotificationCategory(
      identifier: "AGENT_QUESTION",
      actions: [reply, stop],
      intentIdentifiers: [],
      options: []
    )

    UNUserNotificationCenter.current().setNotificationCategories([
      approvalCategory, questionCategory,
    ])
  }

  private func performNotifyCallback(
    token: String,
    action: String,
    text: String?,
    completion: @escaping () -> Void
  ) {
    guard
      let urlStr = UserDefaults.standard.string(forKey: notifyCallbackURLKey),
      let url = URL(string: urlStr)
    else {
      NSLog("[NotifyCallback] missing callback url, dropping action=%@", action)
      completion()
      return
    }

    var body: [String: Any] = ["token": token, "action": action]
    if let text = text { body["text"] = text }
    guard let data = try? JSONSerialization.data(withJSONObject: body) else {
      completion()
      return
    }

    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")

    // Background sessions only support file-based upload tasks.
    let tmpURL = FileManager.default.temporaryDirectory
      .appendingPathComponent("notify_\(UUID().uuidString).json")
    do {
      try data.write(to: tmpURL)
    } catch {
      NSLog("[NotifyCallback] write temp failed: %@", error.localizedDescription)
      completion()
      return
    }

    let task = notifyBgSession.uploadTask(with: request, fromFile: tmpURL)
    task.resume()
    NSLog("[NotifyCallback] dispatched action=%@", action)
    completion()
  }

  override func application(
    _ application: UIApplication,
    handleEventsForBackgroundURLSession identifier: String,
    completionHandler: @escaping () -> Void
  ) {
    if identifier == notifyBgSessionIdentifier {
      notifyBgCompletionHandler = completionHandler
      _ = notifyBgSession  // recreate the session so its delegate fires
    } else {
      super.application(
        application,
        handleEventsForBackgroundURLSession: identifier,
        completionHandler: completionHandler
      )
    }
  }

  private var lastDeliveredTapKey: String?
  private var lastDeliveredAt: Date = .distantPast

  private func notifyPushTap(sessionId: String, messageId: String? = nil, recipientId: String? = nil) {
    // Deduplicate: skip if the same tap key was delivered within the last 3 seconds.
    let now = Date()
    let tapKey = "\(sessionId)|\(messageId ?? "")"
    if lastDeliveredTapKey == tapKey,
       now.timeIntervalSince(lastDeliveredAt) < 3.0 {
      NSLog("[PushTap] dedup: skipping duplicate tap key=%@", tapKey)
      return
    }
    lastDeliveredTapKey = tapKey
    lastDeliveredAt = now
    NSLog("[PushTap] delivering session_id=%@ message_id=%@ channel=%@", sessionId, messageId ?? "", pushTapMethodChannel != nil ? "ok" : "nil")

    guard let channel = pushTapMethodChannel else {
      NSLog("[PushTap] ⚠️ no channel, storing as pending: %@", sessionId)
      pendingTapPayload = [
        "session_id": sessionId,
        "message_id": messageId ?? "",
        "recipient_id": recipientId ?? "",
      ]
      return
    }
    DispatchQueue.main.async {
      NSLog("[PushTap] invoking onPushTapped for session_id=%@ message_id=%@", sessionId, messageId ?? "")
      channel.invokeMethod("onPushTapped", arguments: [
        "session_id": sessionId,
        "message_id": messageId ?? "",
        "recipient_id": recipientId ?? "",
      ])
    }
  }

  private func stringifyPushField(_ value: Any?) -> String? {
    guard let value else { return nil }
    if let text = value as? String {
      let normalized = text.trimmingCharacters(in: .whitespacesAndNewlines)
      return normalized.isEmpty ? nil : normalized
    }
    if let number = value as? NSNumber {
      let normalized = number.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
      return normalized.isEmpty ? nil : normalized
    }
    return nil
  }

  private func registerApplePush(result: @escaping FlutterResult) {
    guard let pushEnv = currentAPNsPushEnv() else {
      result(
        FlutterError(
          code: "apns_env_missing",
          message: "Missing valid APNs environment in Info.plist",
          details: nil
        )
      )
      return
    }

    if !apnsDeviceTokenHex.isEmpty {
      result(pushRegistrationPayload(pushEnv: pushEnv))
      return
    }

    UNUserNotificationCenter.current().getNotificationSettings { [weak self] settings in
      guard let self else { return }
      switch settings.authorizationStatus {
      case .authorized, .provisional, .ephemeral:
        DispatchQueue.main.async {
          self.pendingPushResult = result
          UIApplication.shared.registerForRemoteNotifications()
        }
      case .notDetermined:
        UNUserNotificationCenter.current().requestAuthorization(
          options: [.alert, .badge, .sound]
        ) { granted, error in
          if let error {
            result(
              FlutterError(
                code: "apns_permission_error",
                message: error.localizedDescription,
                details: nil
              )
            )
            return
          }
          guard granted else {
            result(
              FlutterError(
                code: "permission_denied",
                message: "Notification permission denied",
                details: nil
              )
            )
            return
          }
          DispatchQueue.main.async {
            self.pendingPushResult = result
            UIApplication.shared.registerForRemoteNotifications()
          }
        }
      default:
        result(
          FlutterError(
            code: "permission_denied",
            message: "Notification permission denied",
            details: nil
          )
        )
      }
    }
  }

  private var activeKeyWindow: UIWindow? {
    if let w = window, w.rootViewController != nil { return w }
    return UIApplication.shared.connectedScenes
      .compactMap { $0 as? UIWindowScene }
      .flatMap { $0.windows }
      .first { $0.isKeyWindow }
  }

  private func signInWithGoogle(result: @escaping FlutterResult) {
    let clientID = infoString(forKey: "GIDClientID")
    let serverClientID = infoString(forKey: "GIDServerClientID")
    guard !clientID.isEmpty, !serverClientID.isEmpty else {
      result(
        FlutterError(
          code: "google_config_missing",
          message: "Missing GIDClientID or GIDServerClientID in Info.plist",
          details: nil
        )
      )
      return
    }
    guard let presentingViewController = activeKeyWindow?.rootViewController else {
      result(
        FlutterError(
          code: "sign_in_failed",
          message: "Missing root view controller for Google sign-in",
          details: nil
        )
      )
      return
    }

    GIDSignIn.sharedInstance.configuration = GIDConfiguration(
      clientID: clientID,
      serverClientID: serverClientID
    )
    GIDSignIn.sharedInstance.signIn(withPresenting: presentingViewController) {
      signInResult, error in
      if let nsError = error as NSError? {
        if nsError.code == GIDSignInError.canceled.rawValue {
          result(
            FlutterError(
              code: "sign_in_cancelled",
              message: "Google sign-in was cancelled",
              details: nil
            )
          )
          return
        }
        result(
          FlutterError(
            code: "sign_in_failed",
            message: nsError.localizedDescription,
            details: nil
          )
        )
        return
      }

      let idToken = signInResult?.user.idToken?.tokenString
        .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
      guard !idToken.isEmpty else {
        result(
          FlutterError(
            code: "sign_in_failed",
            message: "Google ID token is empty",
            details: nil
          )
        )
        return
      }

      result([
        "idToken": idToken,
      ])
    }
  }

  private func infoString(forKey key: String) -> String {
    return (Bundle.main.object(forInfoDictionaryKey: key) as? String)?
      .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
  }

  private func signInWithApple(result: @escaping FlutterResult) {
    guard let presentingViewController = activeKeyWindow?.rootViewController else {
      result(
        FlutterError(
          code: "sign_in_failed",
          message: "Missing root view controller for Apple sign-in",
          details: nil
        )
      )
      return
    }

    let provider = ASAuthorizationAppleIDProvider()
    let request = provider.createRequest()
    request.requestedScopes = [.fullName, .email]

    let delegate = AppleSignInDelegate(result: result) { [weak self] in
      self?.appleSignInDelegate = nil
    }
    appleSignInDelegate = delegate

    let controller = ASAuthorizationController(authorizationRequests: [request])
    controller.delegate = delegate
    controller.presentationContextProvider = self
    controller.performRequests()
  }

  private func setAppBadge(call: FlutterMethodCall, result: @escaping FlutterResult) {
    guard
      let args = call.arguments as? [String: Any],
      let count = parseBadgeCount(args["count"])
    else {
      result(
        FlutterError(
          code: "invalid_args",
          message: "Missing valid count",
          details: nil
        )
      )
      return
    }

    let normalized = max(0, count)
    DispatchQueue.main.async {
      if #available(iOS 16.0, *) {
        UNUserNotificationCenter.current().setBadgeCount(normalized) { error in
          if let error {
            result(
              FlutterError(
                code: "set_badge_failed",
                message: error.localizedDescription,
                details: nil
              )
            )
            return
          }
          result(nil)
        }
        return
      }
      UIApplication.shared.applicationIconBadgeNumber = normalized
      result(nil)
    }
  }

  private func releaseAudioSession(result: @escaping FlutterResult) {
    DispatchQueue.main.async {
      do {
        try AVAudioSession.sharedInstance().setActive(
          false,
          options: [.notifyOthersOnDeactivation]
        )
        result(nil)
      } catch {
        result(
          FlutterError(
            code: "audio_session_release_failed",
            message: error.localizedDescription,
            details: nil
          )
        )
      }
    }
  }

  private func parseBadgeCount(_ rawValue: Any?) -> Int? {
    if let value = rawValue as? Int {
      return value
    }
    if let value = rawValue as? NSNumber {
      return value.intValue
    }
    return nil
  }

  private func pushRegistrationPayload(pushEnv: String) -> [String: String] {
    [
      "platform": "ios",
      "pushEnv": pushEnv,
      "deviceToken": apnsDeviceTokenHex
    ]
  }

  private func currentAPNsPushEnv() -> String? {
    let rawValue = (Bundle.main.object(forInfoDictionaryKey: apnsEnvironmentInfoKey) as? String)?
      .trimmingCharacters(in: .whitespacesAndNewlines)
      .lowercased()

    switch rawValue {
    case "development":
      return "apns_sandbox"
    case "production":
      return "apns_production"
    default:
      return nil
    }
  }

  private func saveVideoToPhotoLibrary(
    filePath: String,
    result: @escaping FlutterResult
  ) {
    let fileURL = URL(fileURLWithPath: filePath)
    let saveBlock = {
      PHPhotoLibrary.shared().performChanges({
        let request = PHAssetCreationRequest.forAsset()
        let options = PHAssetResourceCreationOptions()
        // 移动而非拷贝：省一次大文件 IO，且系统移走后临时文件自动消失。
        options.shouldMoveFile = true
        request.addResource(with: .video, fileURL: fileURL, options: options)
      }) { success, error in
        DispatchQueue.main.async {
          if let error {
            result(
              FlutterError(
                code: "save_failed",
                message: error.localizedDescription,
                details: nil
              )
            )
            return
          }
          if success {
            result("system_gallery")
            return
          }
          result(
            FlutterError(
              code: "save_failed",
              message: "Unknown photo library error",
              details: nil
            )
          )
        }
      }
    }

    if #available(iOS 14, *) {
      let status = PHPhotoLibrary.authorizationStatus(for: .addOnly)
      switch status {
      case .authorized, .limited:
        saveBlock()
      case .notDetermined:
        PHPhotoLibrary.requestAuthorization(for: .addOnly) { status in
          DispatchQueue.main.async {
            if status == .authorized || status == .limited {
              saveBlock()
            } else {
              result(
                FlutterError(
                  code: "permission_denied",
                  message: "Photo library add permission denied",
                  details: nil
                )
              )
            }
          }
        }
      default:
        result(
          FlutterError(
            code: "permission_denied",
            message: "Photo library add permission denied",
            details: nil
          )
        )
      }
      return
    }

    let status = PHPhotoLibrary.authorizationStatus()
    switch status {
    case .authorized:
      saveBlock()
    case .notDetermined:
      PHPhotoLibrary.requestAuthorization { status in
        DispatchQueue.main.async {
          if status == .authorized {
            saveBlock()
          } else {
            result(
              FlutterError(
                code: "permission_denied",
                message: "Photo library permission denied",
                details: nil
              )
            )
          }
        }
      }
    default:
      result(
        FlutterError(
          code: "permission_denied",
          message: "Photo library permission denied",
          details: nil
        )
      )
    }
  }

  private func saveImageToPhotoLibrary(
    bytes: Data,
    fileName: String,
    result: @escaping FlutterResult
  ) {
    let saveBlock = {
      PHPhotoLibrary.shared().performChanges({
        let request = PHAssetCreationRequest.forAsset()
        let options = PHAssetResourceCreationOptions()
        options.originalFilename = fileName
        request.addResource(with: .photo, data: bytes, options: options)
      }) { success, error in
        DispatchQueue.main.async {
          if let error {
            result(
              FlutterError(
                code: "save_failed",
                message: error.localizedDescription,
                details: nil
              )
            )
            return
          }
          if success {
            result("system_gallery")
            return
          }
          result(
            FlutterError(
              code: "save_failed",
              message: "Unknown photo library error",
              details: nil
            )
          )
        }
      }
    }

    if #available(iOS 14, *) {
      let status = PHPhotoLibrary.authorizationStatus(for: .addOnly)
      switch status {
      case .authorized, .limited:
        saveBlock()
      case .notDetermined:
        PHPhotoLibrary.requestAuthorization(for: .addOnly) { status in
          DispatchQueue.main.async {
            if status == .authorized || status == .limited {
              saveBlock()
            } else {
              result(
                FlutterError(
                  code: "permission_denied",
                  message: "Photo library add permission denied",
                  details: nil
                )
              )
            }
          }
        }
      default:
        result(
          FlutterError(
            code: "permission_denied",
            message: "Photo library add permission denied",
            details: nil
          )
        )
      }
      return
    }

    let status = PHPhotoLibrary.authorizationStatus()
    switch status {
    case .authorized:
      saveBlock()
    case .notDetermined:
      PHPhotoLibrary.requestAuthorization { status in
        DispatchQueue.main.async {
          if status == .authorized {
            saveBlock()
          } else {
            result(
              FlutterError(
                code: "permission_denied",
                message: "Photo library permission denied",
                details: nil
              )
            )
          }
        }
      }
    default:
      result(
        FlutterError(
          code: "permission_denied",
          message: "Photo library permission denied",
          details: nil
        )
      )
    }
  }
}

// MARK: - ASAuthorizationControllerPresentationContextProviding

extension AppDelegate: ASAuthorizationControllerPresentationContextProviding {
  func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
    return activeKeyWindow ?? UIWindow()
  }
}

// MARK: - URLSessionDelegate (offline notification callback)

extension AppDelegate: URLSessionDelegate {
  func urlSessionDidFinishEvents(forBackgroundURLSession session: URLSession) {
    DispatchQueue.main.async { [weak self] in
      self?.notifyBgCompletionHandler?()
      self?.notifyBgCompletionHandler = nil
    }
  }
}

extension AppDelegate: URLSessionTaskDelegate {
  func urlSession(
    _ session: URLSession,
    task: URLSessionTask,
    didCompleteWithError error: Error?
  ) {
    if let error {
      NSLog("[NotifyCallback] task error: %@", error.localizedDescription)
    } else if let resp = task.response as? HTTPURLResponse {
      NSLog("[NotifyCallback] task done status=%ld", resp.statusCode)
    }
  }
}

// MARK: - AppleSignInDelegate

private class AppleSignInDelegate: NSObject, ASAuthorizationControllerDelegate {
  private let result: FlutterResult
  private let cleanup: () -> Void

  init(result: @escaping FlutterResult, cleanup: @escaping () -> Void) {
    self.result = result
    self.cleanup = cleanup
  }

  func authorizationController(
    controller: ASAuthorizationController,
    didCompleteWithAuthorization authorization: ASAuthorization
  ) {
    defer { cleanup() }
    guard let credential = authorization.credential as? ASAuthorizationAppleIDCredential else {
      result(
        FlutterError(code: "sign_in_failed", message: "Unexpected credential type", details: nil)
      )
      return
    }

    guard let identityTokenData = credential.identityToken,
          let idToken = String(data: identityTokenData, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines),
          !idToken.isEmpty
    else {
      result(
        FlutterError(code: "sign_in_failed", message: "Apple ID token is empty", details: nil)
      )
      return
    }

    result(["idToken": idToken])
  }

  func authorizationController(
    controller: ASAuthorizationController,
    didCompleteWithError error: Error
  ) {
    defer { cleanup() }
    if let authError = error as? ASAuthorizationError, authError.code == .canceled {
      result(
        FlutterError(code: "sign_in_cancelled", message: "Apple sign-in was cancelled", details: nil)
      )
      return
    }
    result(
      FlutterError(code: "sign_in_failed", message: error.localizedDescription, details: nil)
    )
  }
}
