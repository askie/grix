import Combine
import Foundation
import SwiftUI
import WidgetKit

/// 手表与复杂功能共享计数的容器。凭证不进这里 —— 它留在手表 App 自己的钥匙串，
/// 复杂功能只需要两个数字。
enum WatchSharedCounts {
  static let appGroup = "group.pub.dhf.grix.watch"
  static let pendingKey = "pending_count"
  static let runningKey = "running_count"

  static func write(pending: Int, running: Int) {
    guard let defaults = UserDefaults(suiteName: appGroup) else { return }
    defaults.set(pending, forKey: pendingKey)
    defaults.set(running, forKey: runningKey)
    WidgetCenter.shared.reloadAllTimelines()
  }

  static func read() -> (pending: Int, running: Int) {
    guard let defaults = UserDefaults(suiteName: appGroup) else { return (0, 0) }
    return (defaults.integer(forKey: pendingKey), defaults.integer(forKey: runningKey))
  }
}

/// 全部页面共用的一份状态。一次 `chat_states/list` 同时喂收件箱、agent 列表
/// 和复杂功能计数。
@MainActor
final class WatchStore: ObservableObject {
  @Published private(set) var states: [ChatState] = []
  @Published private(set) var isLoading = false
  @Published var errorMessage: String?
  @Published private(set) var needsResync = false

  private let provider = WatchCredentialProvider.shared
  private var credentialsObserver: AnyCancellable?
  /// 每收到一份新凭证 +1。在飞的那次刷新用的是旧凭证，它的失败不作数。
  private var credentialsGeneration = 0

  init() {
    // 手机补推凭证后要自己恢复，不该让用户去重启手表 App。
    credentialsObserver = provider.$credentials
      .dropFirst()
      .sink { [weak self] next in
        guard next.isUsable else { return }
        Task { @MainActor [weak self] in
          guard let self else { return }
          self.credentialsGeneration &+= 1
          self.needsResync = false
          self.errorMessage = nil
          await self.refresh()
        }
      }
  }

  var inbox: [ChatState] { states.filter(\.isWaiting) }

  var agents: [ChatState] {
    // 每个 agent 只留最近活动的一行。
    var seen = Set<String>()
    return states.filter { seen.insert($0.agentID).inserted }
  }

  /// 按会话列，不按 agent 去重：一个 agent 名下同时跑几条会话时，每条都要能选中。
  /// `states` 已按活动时间倒序，取前 50 条够手表上翻的了。
  var recentSessions: [ChatState] { Array(states.prefix(50)) }

  /// 所有网络调用的唯一入口：先保证 token 没临期，被拒一次就用手表自己的
  /// refresh token 续一次再重试。refresh 也失效才算真的要回手机同步。
  private func call<T>(_ operation: (GrixAPI) async throws -> T) async throws -> T {
    let credentials = await provider.usableCredentials()
    guard credentials.isUsable else { throw GrixAPIError.notConfigured }
    do {
      return try await operation(GrixAPI(credentials: credentials))
    } catch GrixAPIError.unauthorized {
      let renewed = try await provider.renew()
      return try await operation(GrixAPI(credentials: renewed))
    }
  }

  /// 会话里最后一条 agent 纯文本回复。取不到就不显示，不打扰主流程。
  func lastAgentReply(sessionID: String) async -> String? {
    try? await call { try await $0.lastAgentReply(sessionID: sessionID) }
  }

  func refresh() async {
    guard !isLoading else { return }
    isLoading = true
    let generation = credentialsGeneration
    do {
      let rows = try await call { try await $0.listChatStates(waitingOnly: false) }
      states = rows.sorted { $0.updatedAt > $1.updatedAt }
      needsResync = false
      errorMessage = nil
      WatchSharedCounts.write(
        pending: rows.filter(\.isWaiting).count,
        running: rows.filter(\.isRunning).count
      )
    } catch {
      // 期间手机推来了新凭证：这次拿的是旧的，失败不作数，别再把界面打回
      // "回手机同步"。
      if generation == credentialsGeneration {
        handle(error)
      }
    }
    isLoading = false
    if generation != credentialsGeneration {
      await refresh()
    }
  }

  /// 执行一个主人动作，成功后立刻刷新，让已处理的待办从列表里消失。
  func perform(_ action: String, on state: ChatState, text: String? = nil) async -> Bool {
    do {
      try await call {
        try await $0.ownerAction(sessionID: state.sessionID, action: action, text: text)
      }
      errorMessage = nil
      await refresh()
      return true
    } catch {
      handle(error)
      // 服务端说这条已经不在等待中：刷新一次把它清掉。
      if case GrixAPIError.stale = error { await refresh() }
      return false
    }
  }

  private func handle(_ error: Error) {
    if case GrixAPIError.unauthorized = error {
      needsResync = true
    }
    if case GrixAPIError.notConfigured = error {
      needsResync = true
    }
    // 凭证没有或已经彻底失效：除了提示用户，也向手机要一份新的——它多半还登着，
    // 只是从没为这只手表签发过。
    if needsResync {
      provider.requestCredentialsFromPhone()
    }
    errorMessage = error.localizedDescription
  }
}
