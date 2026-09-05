import Foundation
import WatchConnectivity

/// 手表侧的凭证：只有 access token，没有 refresh token。
///
/// refresh token 每次使用都会轮转并作废整条家族，手表和手机共用会互相踢下线，
/// 所以手表不刷新凭证：token 过期就提示用户回手机打开一次 Grix。
struct WatchCredentials: Equatable {
  var accessToken: String
  var apiBaseURL: String
  var wsBaseURL: String
  var expiresAtMs: Int64

  static let empty = WatchCredentials(accessToken: "", apiBaseURL: "", wsBaseURL: "", expiresAtMs: 0)

  var isUsable: Bool {
    !accessToken.isEmpty && !apiBaseURL.isEmpty && !wsBaseURL.isEmpty
  }
}

/// 钥匙串存取。手表重启、App 被杀之后凭证仍在，直到手机推来新的一份或退出登录。
enum WatchCredentialKeychain {
  private static let service = "pub.dhf.grix.watch"
  private static let account = "credentials"

  private struct Stored: Codable {
    let accessToken: String
    let apiBaseURL: String
    let wsBaseURL: String
    let expiresAtMs: Int64
  }

  static func load() -> WatchCredentials {
    let query: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrAccount as String: account,
      kSecReturnData as String: true,
      kSecMatchLimit as String: kSecMatchLimitOne,
    ]
    var item: CFTypeRef?
    guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
          let data = item as? Data,
          let stored = try? JSONDecoder().decode(Stored.self, from: data)
    else {
      return .empty
    }
    return WatchCredentials(
      accessToken: stored.accessToken,
      apiBaseURL: stored.apiBaseURL,
      wsBaseURL: stored.wsBaseURL,
      expiresAtMs: stored.expiresAtMs
    )
  }

  static func save(_ credentials: WatchCredentials) {
    let base: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrAccount as String: account,
    ]
    SecItemDelete(base as CFDictionary)
    guard credentials.isUsable,
          let data = try? JSONEncoder().encode(Stored(
            accessToken: credentials.accessToken,
            apiBaseURL: credentials.apiBaseURL,
            wsBaseURL: credentials.wsBaseURL,
            expiresAtMs: credentials.expiresAtMs
          ))
    else {
      // 空凭证 = 手机已退出登录，删除即可。
      return
    }
    var attributes = base
    attributes[kSecValueData as String] = data
    attributes[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
    SecItemAdd(attributes as CFDictionary, nil)
  }
}

/// 接收手机通过 `WCSession.updateApplicationContext` 推来的凭证。
@MainActor
final class WatchCredentialProvider: NSObject, ObservableObject {
  static let shared = WatchCredentialProvider()

  @Published private(set) var credentials: WatchCredentials = WatchCredentialKeychain.load()

  private override init() {
    super.init()
  }

  func activate() {
    guard WCSession.isSupported() else { return }
    let session = WCSession.default
    session.delegate = self
    session.activate()
    // 激活时补读一次：手表 App 冷启动时手机可能早就推过凭证了。
    apply(session.receivedApplicationContext)
  }

  nonisolated func apply(_ context: [String: Any]) {
    let next = WatchCredentials(
      accessToken: context["access_token"] as? String ?? "",
      apiBaseURL: context["api_base_url"] as? String ?? "",
      wsBaseURL: context["ws_base_url"] as? String ?? "",
      expiresAtMs: (context["access_expires_at_ms"] as? NSNumber)?.int64Value ?? 0
    )
    guard !context.isEmpty else { return }
    Task { @MainActor in
      guard next != self.credentials else { return }
      WatchCredentialKeychain.save(next)
      self.credentials = next
    }
  }
}

extension WatchCredentialProvider: WCSessionDelegate {
  nonisolated func session(
    _ session: WCSession,
    activationDidCompleteWith activationState: WCSessionActivationState,
    error: Error?
  ) {
    guard error == nil else { return }
    apply(session.receivedApplicationContext)
  }

  nonisolated func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String: Any]) {
    apply(applicationContext)
  }
}
