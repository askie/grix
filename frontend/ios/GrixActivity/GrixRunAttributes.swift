import ActivityKit
import Foundation

/// 一次 agent 运行的实时活动。这个文件同时编进 Runner 和 GrixActivity 两个 target：
/// 卡片由扩展渲染，token 由 App 上报，两边必须是同一个类型。
///
/// 字段名与后端 `protocol.LiveActivityAttributes` /
/// `protocol.LiveActivityContentState` 的 JSON 一一对应 —— APNs 下发的
/// `aps.attributes` 和 `aps.content-state` 由系统直接解成这个类型，键名对不上
/// 整条推送会被静默丢掉。
@available(iOS 16.2, *)
struct GrixRunAttributes: ActivityAttributes {
  /// 卡片上会变的那部分。每次推送都带全量。
  struct ContentState: Codable, Hashable {
    /// 取值与后端 chat_states 的状态名一致，外加 stopped。
    /// 存成字符串而不是枚举：后端将来加一个状态时，旧版本 App 该退化成默认样式，
    /// 而不是因为解不出枚举把整条更新丢掉、把卡片永远冻在上一帧。
    var phase: String
    /// 任务标题（会话标题）。
    var title: String
    /// 副标题：等待原因、失败原因等。
    var detail: String
    var updatedAtMs: Int64

    enum CodingKeys: String, CodingKey {
      case phase
      case title
      case detail
      case updatedAtMs = "updated_at_ms"
    }
  }

  var sessionId: String
  var agentId: String
  var agentName: String

  enum CodingKeys: String, CodingKey {
    case sessionId = "session_id"
    case agentId = "agent_id"
    case agentName = "agent_name"
  }
}

/// 卡片上的阶段。从 `ContentState.phase` 解出来，认不出的一律按 running 显示
/// ——「还在跑」是未知状态下唯一不会误导人的说法。
enum GrixRunPhase: String {
  case running
  case waitingApproval = "waiting_approval"
  case waitingQuestion = "waiting_question"
  case completed
  case failed
  case stopped

  init(rawPhase: String) {
    self = GrixRunPhase(rawValue: rawPhase) ?? .running
  }

  var isWaiting: Bool {
    self == .waitingApproval || self == .waitingQuestion
  }

  var isFinished: Bool {
    self == .completed || self == .failed || self == .stopped
  }
}
