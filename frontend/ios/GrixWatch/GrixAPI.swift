import Foundation

/// chat_states 的一行：手表的收件箱、agent 列表和复杂功能都由它渲染。
struct ChatState: Codable, Identifiable, Equatable {
  let sessionID: String
  let agentID: String
  let agentName: String
  let agentOnline: Bool
  let agentProviderType: Int
  let state: String
  let taskTitle: String
  let updatedAt: Int64

  var id: String { sessionID }

  enum CodingKeys: String, CodingKey {
    case sessionID = "session_id"
    case agentID = "agent_id"
    case agentName = "agent_name"
    case agentOnline = "agent_online"
    case agentProviderType = "agent_provider_type"
    case state
    case taskTitle = "task_title"
    case updatedAt = "updated_at"
  }

  var isWaiting: Bool {
    state == "waiting_approval" || state == "waiting_question"
  }

  var isRunning: Bool { state == "running" }

  /// 只有连接器托管的 agent 才上报在线状态；远程模型（provider_type=1）永远
  /// 没有连接，不该显示成"离线"。
  var reportsPresence: Bool { agentProviderType != 1 }

  var displayTitle: String {
    taskTitle.isEmpty ? agentName : taskTitle
  }

  var stateLabel: String {
    switch state {
    case "running": return "运行中"
    case "waiting_approval": return "待审批"
    case "waiting_question": return "待回答"
    case "completed": return "已完成"
    case "failed": return "失败"
    default: return "空闲"
    }
  }

  /// 距离最后一次活动多久。只在渲染那一刻算一次，列表靠下拉刷新更新，
  /// 不为了让它走字而挂定时器。
  var relativeUpdatedText: String {
    let seconds = Int(Date().timeIntervalSince1970 - Double(updatedAt) / 1000)
    switch seconds {
    case ..<60: return "刚刚"
    case ..<3600: return "\(seconds / 60)分钟前"
    case ..<86400: return "\(seconds / 3600)小时前"
    default: return "\(seconds / 86400)天前"
    }
  }
}

struct ChatMessage: Codable, Identifiable {
  let msgID: String
  let senderType: Int
  let msgType: Int
  let content: String
  /// 撤回的消息服务端仍会带原文返回，客户端自己不显示（手机端同样口径）。
  let isRevoked: Bool?

  var id: String { msgID }

  /// 1:主人 2:agent，其余（系统通知等）不归任何一方。
  var isFromOwner: Bool { senderType == 1 }
  var isFromAgent: Bool { senderType == 2 }

  /// 本地乐观回显：刚发出、还没从服务端读回来的那句。
  static func localEcho(_ content: String) -> ChatMessage {
    ChatMessage(
      msgID: "local-\(UUID().uuidString)",
      senderType: 1,
      msgType: 1,
      content: content,
      isRevoked: false
    )
  }

  enum CodingKeys: String, CodingKey {
    case msgID = "msg_id"
    case senderType = "sender_type"
    case msgType = "msg_type"
    case content
    case isRevoked = "is_revoked"
  }
}

enum GrixAPIError: LocalizedError {
  /// access token 被拒且 refresh 也救不回来（家族被撤销：手机退出登录、改密、
  /// 重新签发）。只能提示回手机再同步一次。
  case unauthorized
  /// 该待办已经不在等待中（手机上先处理掉了）。
  case stale(String)
  case server(String)
  case notConfigured

  var errorDescription: String? {
    switch self {
    case .unauthorized:
      return "打开 iPhone 上的 Grix 重新同步"
    case .stale(let message):
      return message.isEmpty ? "这条已经处理过了" : message
    case .server(let message):
      return message.isEmpty ? "操作失败" : message
    case .notConfigured:
      return "打开 iPhone 上的 Grix 重新同步"
    }
  }
}

/// 手表直连后端：读走 api 服务，写走 ws 服务（只有 ws 服务持有 agent 连接）。
/// 不连 WebSocket —— watchOS 后台网络受限，前台轮询 + 镜像推送已经够用。
struct GrixAPI {
  let credentials: WatchCredentials

  private var authorized: [String: String] {
    ["Authorization": "Bearer \(credentials.accessToken)"]
  }

  /// 用手表自己的 refresh token 换一对新的。走的是和手机一样的
  /// `POST /v1/auth/refresh`，只是家族不同，所以两边轮转互不干扰。
  func renewTokens() async throws -> WatchCredentials {
    guard credentials.canRenew,
          let url = URL(string: credentials.apiBaseURL + "/auth/refresh")
    else {
      throw GrixAPIError.unauthorized
    }
    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.timeoutInterval = 20
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    request.httpBody = try JSONSerialization.data(
      withJSONObject: ["refresh_token": credentials.refreshToken]
    )

    let (data, response) = try await URLSession.shared.data(for: request)
    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
    if status == 401 { throw GrixAPIError.unauthorized }

    struct Envelope: Codable {
      let code: Int
      let data: Payload?
      struct Payload: Codable {
        let accessToken: String
        let refreshToken: String
        let expiresIn: Int64

        enum CodingKeys: String, CodingKey {
          case accessToken = "access_token"
          case refreshToken = "refresh_token"
          case expiresIn = "expires_in"
        }
      }
    }
    // 5xx 是服务端抖动，不是凭证失效：不能因此让用户回手机"重新同步"。
    guard status == 200,
          let envelope = try? JSONDecoder().decode(Envelope.self, from: data),
          envelope.code == 0,
          let payload = envelope.data,
          !payload.accessToken.isEmpty,
          !payload.refreshToken.isEmpty
    else {
      throw GrixAPIError.server("续期失败（\(status)）")
    }

    var next = credentials
    next.accessToken = payload.accessToken
    next.refreshToken = payload.refreshToken
    next.expiresAtMs = Int64(Date().timeIntervalSince1970 * 1000)
      + max(payload.expiresIn, 1) * 1000
    return next
  }

  func listChatStates(waitingOnly: Bool) async throws -> [ChatState] {
    guard credentials.isUsable else { throw GrixAPIError.notConfigured }
    var path = "/chat_states/list"
    if waitingOnly { path += "?state=waiting" }
    guard let url = URL(string: credentials.apiBaseURL + path) else {
      throw GrixAPIError.notConfigured
    }
    struct Envelope: Codable {
      let code: Int
      let msg: String?
      let data: Payload?
      struct Payload: Codable { let list: [ChatState]? }
    }
    let envelope: Envelope = try await get(url)
    guard envelope.code == 0 else { throw GrixAPIError.server(envelope.msg ?? "") }
    return envelope.data?.list ?? []
  }

  /// 会话最近的纯文本消息（快速发送页用来确认"说到哪了"）。
  /// 历史接口按 msg_id 倒序返回（最新在前），这里翻成正序给界面用：
  /// 最旧在前，最新在最后。
  func recentMessages(sessionID: String, limit: Int = 20) async throws -> [ChatMessage] {
    guard credentials.isUsable,
          let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed),
          let url = URL(string: credentials.apiBaseURL + "/messages/history?session_id=\(encoded)&limit=\(limit)")
    else {
      throw GrixAPIError.notConfigured
    }
    struct Envelope: Codable {
      let code: Int
      let data: Payload?
      struct Payload: Codable { let messages: [ChatMessage]? }
    }
    let envelope: Envelope = try await get(url)
    // 手表只渲染文本：图片、系统通知、流式片段在这块小屏上没有可读的形态。
    let visible = (envelope.data?.messages ?? []).filter {
      $0.msgType == 1 && !$0.content.isEmpty && $0.isRevoked != true
    }
    return visible.reversed()
  }

  /// approve / deny / stop / reply / send，全部由 ws 服务执行。
  func ownerAction(sessionID: String, action: String, text: String? = nil) async throws {
    guard credentials.isUsable,
          let url = URL(string: credentials.wsBaseURL + "/v1/owner-action")
    else {
      throw GrixAPIError.notConfigured
    }
    var body: [String: Any] = ["session_id": sessionID, "action": action]
    if let text, !text.isEmpty { body["text"] = text }

    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.timeoutInterval = 20
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    authorized.forEach { request.setValue($0.value, forHTTPHeaderField: $0.key) }
    request.httpBody = try JSONSerialization.data(withJSONObject: body)

    let (data, response) = try await URLSession.shared.data(for: request)
    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
    struct Reply: Codable {
      let ok: Bool?
      let message: String?
    }
    let reply = try? JSONDecoder().decode(Reply.self, from: data)
    switch status {
    case 200:
      return
    case 401:
      throw GrixAPIError.unauthorized
    case 403, 409:
      throw GrixAPIError.stale(reply?.message ?? "")
    default:
      throw GrixAPIError.server(reply?.message ?? "操作失败（\(status)）")
    }
  }

  private func get<T: Decodable>(_ url: URL) async throws -> T {
    var request = URLRequest(url: url)
    request.timeoutInterval = 20
    authorized.forEach { request.setValue($0.value, forHTTPHeaderField: $0.key) }
    let (data, response) = try await URLSession.shared.data(for: request)
    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
    if status == 401 { throw GrixAPIError.unauthorized }
    guard status == 200 else { throw GrixAPIError.server("请求失败（\(status)）") }
    return try JSONDecoder().decode(T.self, from: data)
  }
}
