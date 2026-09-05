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

  var inbox: [ChatState] { states.filter(\.isWaiting) }

  var agents: [ChatState] {
    // 每个 agent 只留最近活动的一行。
    var seen = Set<String>()
    return states.filter { seen.insert($0.agentID).inserted }
  }

  var api: GrixAPI { GrixAPI(credentials: provider.credentials) }

  func refresh() async {
    guard !isLoading else { return }
    isLoading = true
    defer { isLoading = false }
    do {
      let rows = try await api.listChatStates(waitingOnly: false)
      states = rows.sorted { $0.updatedAt > $1.updatedAt }
      needsResync = false
      errorMessage = nil
      WatchSharedCounts.write(
        pending: rows.filter(\.isWaiting).count,
        running: rows.filter(\.isRunning).count
      )
    } catch {
      handle(error)
    }
  }

  /// 执行一个主人动作，成功后立刻刷新，让已处理的待办从列表里消失。
  func perform(_ action: String, on state: ChatState, text: String? = nil) async -> Bool {
    do {
      try await api.ownerAction(sessionID: state.sessionID, action: action, text: text)
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
    errorMessage = error.localizedDescription
  }
}
