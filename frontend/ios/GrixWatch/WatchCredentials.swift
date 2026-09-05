import Foundation
import WatchConnectivity

/// 手表侧的凭证：一对**手表专属**的 access + refresh token。
///
/// 手机登录后调 `POST /v1/auth/watch/issue` 为手表单独签发一条 refresh 家族。
/// refresh token 每次使用都会轮转并作废整条家族，两台设备共用一份会互相踢下线；
/// 各持一份就互不影响，手表因此可以自己续期，不必等手机再登录一次。
struct WatchCredentials: Equatable {
  var accessToken: String
  var refreshToken: String
  var apiBaseURL: String
  var wsBaseURL: String
  var expiresAtMs: Int64

  static let empty = WatchCredentials(
    accessToken: "",
    refreshToken: "",
    apiBaseURL: "",
    wsBaseURL: "",
    expiresAtMs: 0
  )

  var isUsable: Bool {
    !accessToken.isEmpty && !apiBaseURL.isEmpty && !wsBaseURL.isEmpty
  }

  var canRenew: Bool { !refreshToken.isEmpty && !apiBaseURL.isEmpty }

  /// 提前续期的窗口。留 5 分钟余量，免得请求发到一半 token 就过期了。
  private static let renewLeadMs: Int64 = 5 * 60 * 1000

  var isExpiringSoon: Bool {
    guard expiresAtMs > 0 else { return false }
    return Int64(Date().timeIntervalSince1970 * 1000) + Self.renewLeadMs >= expiresAtMs
  }
}

/// 钥匙串存取。手表重启、App 被杀之后凭证仍在，直到手机推来新的一份或退出登录。
enum WatchCredentialKeychain {
  private static let service = "pub.dhf.grix.watch"
  private static let account = "credentials"

  private struct Stored: Codable {
    let accessToken: String
    // 手表凭证在第 1 档只有 access token，旧版存档里没有这个字段。
    let refreshToken: String?
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
      refreshToken: stored.refreshToken ?? "",
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
            refreshToken: credentials.refreshToken,
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

/// 接收手机通过 `WCSession.updateApplicationContext` 推来的凭证，并在 token 临期
/// 或被拒时自己走 `POST /v1/auth/refresh` 续期。
@MainActor
final class WatchCredentialProvider: NSObject, ObservableObject {
  static let shared = WatchCredentialProvider()

  @Published private(set) var credentials: WatchCredentials = WatchCredentialKeychain.load()

  /// 同一时刻只允许一次续期：几个页面同时刷新时，不能各自拿同一枚 refresh token
  /// 去轮转——第二次会被判定为重放，整条家族当场作废。
  private var renewTask: Task<WatchCredentials, Error>?

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
      refreshToken: context["refresh_token"] as? String ?? "",
      apiBaseURL: context["api_base_url"] as? String ?? "",
      wsBaseURL: context["ws_base_url"] as? String ?? "",
      expiresAtMs: (context["access_expires_at_ms"] as? NSNumber)?.int64Value ?? 0
    )
    guard !context.isEmpty else { return }
    Task { @MainActor in
      guard next != self.credentials else { return }
      // 手机推来的永远是最新一份（旧的家族已在服务端撤销），直接覆盖，
      // 包括正在进行的续期结果。
      self.renewTask = nil
      WatchCredentialKeychain.save(next)
      self.credentials = next
    }
  }

  /// 返回一份可用的凭证，临期时先续一次。续期失败不阻塞调用——手上的 token 也许
  /// 还能用几分钟，真被拒了再走 `renew()`。
  func usableCredentials() async -> WatchCredentials {
    let current = credentials
    guard current.isUsable, current.canRenew, current.isExpiringSoon else {
      return current
    }
    return (try? await renew()) ?? current
  }

  /// 401 之后的显式续期。refresh 也失效时抛 `.unauthorized`，由界面提示回手机同步。
  @discardableResult
  func renew() async throws -> WatchCredentials {
    if let existing = renewTask {
      return try await existing.value
    }
    let current = credentials
    guard current.canRenew else { throw GrixAPIError.unauthorized }

    let task = Task<WatchCredentials, Error> {
      let renewed = try await GrixAPI(credentials: current).renewTokens()
      return await MainActor.run { () -> WatchCredentials in
        // 续期期间手机推来了新凭证（用户重新登录了）：那一份更新，而且这条家族
        // 已经在服务端被撤销，不能用续期结果盖掉它。
        guard self.credentials == current else { return self.credentials }
        self.store(renewed)
        return renewed
      }
    }
    renewTask = task
    defer { renewTask = nil }
    return try await task.value
  }

  private func store(_ credentials: WatchCredentials) {
    WatchCredentialKeychain.save(credentials)
    self.credentials = credentials
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
