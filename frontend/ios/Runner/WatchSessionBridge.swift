import Foundation
import UIKit
#if canImport(WatchConnectivity)
import WatchConnectivity
#endif

/// 手机 → 手表的凭证通道。
///
/// 手表拿的是它自己的一对 access + refresh token（`POST /v1/auth/watch/issue`
/// 签发，独立的 refresh 家族）。refresh token 每次使用都会轮转并作废整条家族，
/// 两台设备共用会互相踢下线；各持一份则互不影响，手表可以自己续期。
/// 手机自己的 refresh token 永远不经过这里。
///
/// 用 `updateApplicationContext` 而不是 `sendMessage`：它只保留最新一份、手表不在
/// 线时也会在下次连上时送达，正是「当前凭证」这种状态该有的语义。
final class WatchSessionBridge: NSObject {
  static let shared = WatchSessionBridge()

  /// 与 Dart 端 `WatchCredentialSync` 约定的 payload 键。
  private enum Key {
    static let accessToken = "access_token"
    static let refreshToken = "refresh_token"
    static let apiBaseURL = "api_base_url"
    static let wsBaseURL = "ws_base_url"
    static let expiresAt = "access_expires_at_ms"
    static let updatedAt = "updated_at_ms"
  }

  /// 「手表该有凭证却没有」时的回调，由 AppDelegate 转成 Flutter 的
  /// `ensureCredentials`。参数为 true 表示是手表主动索要——手机若已退出登录，
  /// 那种情况要回一份空凭证让手表丢掉陈旧 token。
  var onCredentialsNeeded: ((_ watchRequested: Bool) -> Void)?

  private override init() {
    super.init()
  }

  func activate() {
    #if canImport(WatchConnectivity)
    guard WCSession.isSupported() else { return }
    let session = WCSession.default
    session.delegate = self
    session.activate()
    NotificationCenter.default.addObserver(
      self,
      selector: #selector(handleAppDidBecomeActive),
      name: UIApplication.didBecomeActiveNotification,
      object: nil
    )
    #endif
  }

  @objc private func handleAppDidBecomeActive() {
    syncIfWatchHasNoCredentials()
  }

  /// 手表已配对、已装 App，但手机上次推出去的那份 applicationContext 里没有
  /// access token —— 说明这台手机从没为这只手表签发过（用户装手表 App 之前就
  /// 已经是登录态，之后再没登录第二次）。这时才去补一次签发。
  ///
  /// 用「上次推出去的 context」判断而不是问手表：它就在本地，冷启动、手表离线
  /// 时同样准，也不会多一次往返。
  func syncIfWatchHasNoCredentials() {
    #if canImport(WatchConnectivity)
    guard WCSession.isSupported() else { return }
    let session = WCSession.default
    guard session.activationState == .activated else {
      // 还没激活：激活完成的回调里会再查一次。
      session.delegate = self
      session.activate()
      return
    }
    guard session.isPaired, session.isWatchAppInstalled else { return }
    let pushed = session.applicationContext[Key.accessToken] as? String ?? ""
    guard pushed.isEmpty else { return }
    onCredentialsNeeded?(false)
    #endif
  }

  /// 同步一份凭证。空 token 表示「已退出登录」，手表收到后清空本地钥匙串。
  func sync(
    accessToken: String,
    refreshToken: String,
    apiBaseURL: String,
    wsBaseURL: String,
    expiresAtMs: Int64
  ) {
    #if canImport(WatchConnectivity)
    guard WCSession.isSupported() else { return }
    let session = WCSession.default
    if session.activationState != .activated {
      session.delegate = self
      session.activate()
    }
    let context: [String: Any] = [
      Key.accessToken: accessToken,
      Key.refreshToken: refreshToken,
      Key.apiBaseURL: apiBaseURL,
      Key.wsBaseURL: wsBaseURL,
      Key.expiresAt: expiresAtMs,
      // 手表用它区分「同一份 token 的重复投递」和「新凭证」。
      Key.updatedAt: Int64(Date().timeIntervalSince1970 * 1000),
    ]
    do {
      try session.updateApplicationContext(context)
    } catch {
      NSLog("[Watch] updateApplicationContext failed: %@", error.localizedDescription)
    }
    #endif
  }

  func clear() {
    sync(accessToken: "", refreshToken: "", apiBaseURL: "", wsBaseURL: "", expiresAtMs: 0)
  }
}

#if canImport(WatchConnectivity)
extension WatchSessionBridge: WCSessionDelegate {
  func session(
    _ session: WCSession,
    activationDidCompleteWith activationState: WCSessionActivationState,
    error: Error?
  ) {
    if let error {
      NSLog("[Watch] session activation failed: %@", error.localizedDescription)
      return
    }
    syncIfWatchHasNoCredentials()
  }

  // 用户刚在手表上装好 Grix：这一刻才第一次有可推送的对象。
  func sessionWatchStateDidChange(_ session: WCSession) {
    syncIfWatchHasNoCredentials()
  }

  // 手表主动索要凭证：可达时走 sendMessage，不可达时手表会改用 transferUserInfo
  // 排队，两条路都落到这里。
  func session(_ session: WCSession, didReceiveMessage message: [String: Any]) {
    handleWatchRequest(message)
  }

  func session(
    _ session: WCSession,
    didReceiveMessage message: [String: Any],
    replyHandler: @escaping ([String: Any]) -> Void
  ) {
    handleWatchRequest(message)
    replyHandler([:])
  }

  func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any]) {
    handleWatchRequest(userInfo)
  }

  private func handleWatchRequest(_ payload: [String: Any]) {
    guard payload["request"] as? String == "request_credentials" else { return }
    onCredentialsNeeded?(true)
  }

  func sessionDidBecomeInactive(_ session: WCSession) {}

  // 换表后必须重新激活，否则新表永远收不到凭证。
  func sessionDidDeactivate(_ session: WCSession) {
    session.activate()
  }
}
#endif
