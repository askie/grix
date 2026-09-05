import Foundation
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

  private override init() {
    super.init()
  }

  func activate() {
    #if canImport(WatchConnectivity)
    guard WCSession.isSupported() else { return }
    let session = WCSession.default
    session.delegate = self
    session.activate()
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
    }
  }

  func sessionDidBecomeInactive(_ session: WCSession) {}

  // 换表后必须重新激活，否则新表永远收不到凭证。
  func sessionDidDeactivate(_ session: WCSession) {
    session.activate()
  }
}
#endif
