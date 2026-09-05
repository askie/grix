import Flutter
import Foundation
#if canImport(ActivityKit)
import ActivityKit
#endif

/// 实时活动的 token 通道。
///
/// 两种 token，来源和用途都不一样：
/// - **启动 token**（push-to-start，每设备一个）：后端拿它把卡片从零推起来。
///   随下一次设备注册（`/devices/bind`）捎带上报。
/// - **活动 token**（每张卡一个）：卡片开出来之后才存在，后端拿它更新和结束这张卡。
///   走 `/v1/live_activities/token`。
///
/// 原生这边只负责"拿到 token 就交给 Flutter"，不碰后端：登录态、baseURL、
/// 重试都在 Dart 侧，原生再实现一遍只会多一处会过期的副本。
final class LiveActivityBridge {
  static let shared = LiveActivityBridge()

  /// 与 Dart 端 `LiveActivityService` 约定的方法名与 payload 键。
  private enum Method {
    static let startToken = "onPushToStartToken"
    static let activityToken = "onActivityToken"
  }

  private var channel: FlutterMethodChannel?
  private var observing = false
  /// 卡片开出来之后系统才发它的 token，但 App 可能这会儿正好没在跑。
  /// 先攒着，Flutter 侧一问就给。
  private var pendingStartToken: String?
  private var pendingActivityTokens: [[String: String]] = []

  private init() {}

  func attach(channel: FlutterMethodChannel) {
    self.channel = channel
    channel.setMethodCallHandler { [weak self] call, result in
      switch call.method {
      case "start":
        self?.startObserving()
        result(nil)
      case "drainPending":
        result(self?.drainPending() ?? [:])
      default:
        result(FlutterMethodNotImplemented)
      }
    }
  }

  /// 订阅两条 token 流。系统在每次 App 启动后都会重发当前有效的 token，
  /// 所以这里不需要自己持久化。
  func startObserving() {
    guard !observing else { return }
    observing = true

    #if canImport(ActivityKit)
    guard #available(iOS 17.2, *) else { return }
    Task.detached { [weak self] in
      for await tokenData in Activity<GrixRunAttributes>.pushToStartTokenUpdates {
        self?.deliverStartToken(hexString(tokenData))
      }
    }
    Task.detached { [weak self] in
      // 已经在跑的卡（App 被杀过一轮、卡还在锁屏上）也要补订阅，
      // 否则这张卡的后续更新永远推不动。
      for activity in Activity<GrixRunAttributes>.activities {
        self?.observeActivity(activity)
      }
      for await activity in Activity<GrixRunAttributes>.activityUpdates {
        self?.observeActivity(activity)
      }
    }
    #endif
  }

  #if canImport(ActivityKit)
  @available(iOS 16.2, *)
  private func observeActivity(_ activity: Activity<GrixRunAttributes>) {
    let sessionId = activity.attributes.sessionId
    let activityId = activity.id
    Task.detached { [weak self] in
      for await tokenData in activity.pushTokenUpdates {
        self?.deliverActivityToken(
          sessionId: sessionId,
          activityId: activityId,
          token: hexString(tokenData)
        )
      }
    }
  }
  #endif

  private func deliverStartToken(_ token: String) {
    guard !token.isEmpty else { return }
    DispatchQueue.main.async { [weak self] in
      guard let self else { return }
      guard let channel = self.channel else {
        self.pendingStartToken = token
        return
      }
      channel.invokeMethod(Method.startToken, arguments: ["token": token])
    }
  }

  private func deliverActivityToken(sessionId: String, activityId: String, token: String) {
    guard !token.isEmpty, !sessionId.isEmpty else { return }
    let payload = [
      "session_id": sessionId,
      "activity_id": activityId,
      "token": token,
    ]
    DispatchQueue.main.async { [weak self] in
      guard let self else { return }
      guard let channel = self.channel else {
        self.pendingActivityTokens.append(payload)
        return
      }
      channel.invokeMethod(Method.activityToken, arguments: payload)
    }
  }

  /// Flutter 侧起来之后主动来取一次积压的 token。
  private func drainPending() -> [String: Any] {
    var out: [String: Any] = [:]
    if let startToken = pendingStartToken {
      out["start_token"] = startToken
      pendingStartToken = nil
    }
    if !pendingActivityTokens.isEmpty {
      out["activity_tokens"] = pendingActivityTokens
      pendingActivityTokens = []
    }
    return out
  }
}

/// APNs 的 device token 一律是十六进制串，ActivityKit 给的是原始 Data。
private func hexString(_ data: Data) -> String {
  data.map { String(format: "%02x", $0) }.joined()
}
