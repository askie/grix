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
  let isDeleted: Bool?

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
      isRevoked: false,
      isDeleted: false
    )
  }

  /// 是不是一条"人看的聊天正文"。
  ///
  /// agent 干活期间产生的工具调用、工具结果、审批卡、状态卡在服务端同样落成
  /// msg_type=1，靠 content 里的卡片协议区分：后端写
  /// `[兜底文案](grix://card/{type}?d={json})`，手机端 ChatMessageCardCodec
  /// 只认 content、不看 extra（见 frontend/lib/modules/chat/message_cards/
  /// services/chat_message_card_codec.dart 的 CARD PROTOCOL 注释）。这里沿用
  /// 同一口径，另外把手机端同样隐藏的内部指令一起挡掉。
  var isPlainChatText: Bool {
    guard msgType == 1, isRevoked != true, isDeleted != true else { return false }
    let normalized = content.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !normalized.isEmpty else { return false }
    return !Self.isCardMessage(normalized) && !Self.isInternalDirective(normalized)
  }

  private static let cardURIPrefix = "grix://card/"

  /// 卡片消息：content 要么整条就是 grix://card URI，要么是一条独立的
  /// `[兜底文案](grix://card/...)` markdown 链接。对应手机端
  /// `_decodeStandaloneGrixCardMessage`。
  private static func isCardMessage(_ normalized: String) -> Bool {
    if hasCardURIBody(normalized) { return true }
    guard normalized.hasPrefix("["), normalized.hasSuffix(")"),
          let separator = normalized.range(of: "](", options: .backwards)
    else {
      return false
    }
    let href = normalized[separator.upperBound..<normalized.index(before: normalized.endIndex)]
    // markdown 链接里的 URI 不允许再出现 ")" 或空白，与手机端正则一致。
    guard !href.contains(")"), href.rangeOfCharacter(from: .whitespacesAndNewlines) == nil
    else {
      return false
    }
    return hasCardURIBody(String(href))
  }

  /// grix://card/ 后面必须真的跟着一个类型段，否则手机端也当普通文本渲染。
  private static func hasCardURIBody(_ value: String) -> Bool {
    guard value.lowercased().hasPrefix(cardURIPrefix) else { return false }
    let body = value.dropFirst(cardURIPrefix.count)
      .prefix { $0 != "#" }
      .prefix { $0 != "?" }
    return !body.trimmingCharacters(in: .whitespaces).isEmpty
  }

  /// 主人点卡片按钮后回给 agent 的那几种指令文本，手机端同样不上屏。
  /// 对应 `ChatMessageCardCodec.isInternalDirectiveMessage`。
  private static func isInternalDirective(_ normalized: String) -> Bool {
    let resolutionPrefix = "[[exec-approval-resolution|"
    if normalized.hasPrefix(resolutionPrefix), normalized.hasSuffix("]]"),
       normalized.count > resolutionPrefix.count + 2 {
      return true
    }
    let tokens = normalized.lowercased()
      .split(whereSeparator: { $0.isWhitespace })
      .map(String.init)
    if tokens.count >= 2, tokens[0] == "/approve" { return true }
    if tokens.count >= 3, tokens[0] == "/grix", tokens[1] == "approval" { return true }
    return isOpenSessionDirective(normalized)
  }

  private static func isOpenSessionDirective(_ normalized: String) -> Bool {
    let prefix = "grix://open/"
    guard normalized.hasPrefix(prefix) else { return false }
    let path = normalized.dropFirst(prefix.count)
      .prefix { $0 != "#" }
      .prefix { $0 != "?" }
    return path == "session" || path == "session/"
  }

  enum CodingKeys: String, CodingKey {
    case msgID = "msg_id"
    case senderType = "sender_type"
    case msgType = "msg_type"
    case content
    case isRevoked = "is_revoked"
    case isDeleted = "is_deleted"
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
  ///
  /// limit 拉的是"未过滤"的原始条数：agent 跑一轮会掺进大量工具/审批卡消息，
  /// 按界面要显示的条数去拉必然不够。多拉一些一次性过滤掉，不做翻页——
  /// 手表上再往前翻本来也没人看，60 条里凑不满就有多少显示多少。
  func recentMessages(sessionID: String, limit: Int = 60) async throws -> [ChatMessage] {
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
    // 手表只渲染文本：图片、系统通知、工具与审批卡在这块小屏上没有可读的形态。
    let visible = (envelope.data?.messages ?? []).filter(\.isPlainChatText)
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
