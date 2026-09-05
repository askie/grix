import SwiftUI

/// 快速发送：挑一条最近活动的会话，听写一句话发过去。
/// 按会话列而不是按 agent 列 —— 一个 agent 同时跑几条会话时，每条都得能选中。
/// 发送走 owner-action 的 `send`，和手机上打字发消息是同一条落库路径。
struct QuickSendView: View {
  @EnvironmentObject private var store: WatchStore

  var body: some View {
    NavigationStack {
      List {
        if store.needsResync {
          ResyncNotice()
        } else if store.recentSessions.isEmpty {
          Text(store.isLoading ? "加载中…" : "还没有可发送的会话")
            .foregroundStyle(.secondary)
        } else {
          ForEach(store.recentSessions) { session in
            NavigationLink(destination: QuickSendComposer(target: session)) {
              QuickSendRow(session: session)
            }
          }
        }
      }
      .navigationTitle("快速发送")
      .refreshable { await store.refresh() }
    }
  }
}

struct QuickSendRow: View {
  let session: ChatState

  var body: some View {
    VStack(alignment: .leading, spacing: 2) {
      Text(session.displayTitle).font(.headline).lineLimit(2)
      Text("\(session.agentName) · \(session.stateLabel) · \(session.relativeUpdatedText)")
        .font(.caption2)
        .foregroundStyle(.secondary)
        .lineLimit(1)
    }
  }
}

struct QuickSendComposer: View {
  let target: ChatState
  @EnvironmentObject private var store: WatchStore
  @StateObject private var speech = SpeechReader()

  /// 发出去之后等回复的轮询节奏。抬腕看一眼的时间量级就够了，再久用户早就
  /// 放下手表了——不值得为它一直占着网络和电。
  private static let replyPollInterval: UInt64 = 5_000_000_000
  private static let replyPollRounds = 12
  /// 表盘就这么大：再多几条也翻不完，反而把输入框顶出视野。
  private static let visibleMessageCount = 8

  @State private var text = ""
  @State private var messages: [ChatMessage] = []
  @State private var isSending = false
  @State private var isAwaitingReply = false
  @State private var sentNotice: String?
  @State private var pollTask: Task<Void, Never>?

  private var trimmed: String {
    text.trimmingCharacters(in: .whitespacesAndNewlines)
  }

  /// 时间正序，最新的一条在最下面。
  private var visibleMessages: [ChatMessage] {
    Array(messages.suffix(Self.visibleMessageCount))
  }

  /// 朗读和"有没有新回复"都只看最后一条 agent 消息。
  private var latestAgentReply: String? {
    messages.last(where: \.isFromAgent)?.content
  }

  private func senderLabel(_ message: ChatMessage) -> String {
    if message.isFromOwner { return "我" }
    if message.isFromAgent { return target.agentName }
    return "系统"
  }

  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 10) {
        Text(target.displayTitle).font(.headline).lineLimit(2)
        Text(target.agentName).font(.caption2).foregroundStyle(.secondary).lineLimit(1)

        TextField("说出要发送的内容", text: $text)
        Button {
          send()
        } label: {
          Label(isSending ? "发送中…" : "发送", systemImage: "paperplane")
        }
        .disabled(trimmed.isEmpty || isSending)

        if let sentNotice {
          Text(sentNotice).font(.caption2).foregroundStyle(.secondary)
        }

        if !visibleMessages.isEmpty {
          // 直接铺在外层 ScrollView 里，不再套一层可滚动容器：手表上两层滚动
          // 会互相抢手势。
          VStack(alignment: .leading, spacing: 8) {
            Text(isAwaitingReply ? "等待新回复…" : "最近消息")
              .font(.caption2)
              .foregroundStyle(.secondary)
            ForEach(visibleMessages) { message in
              VStack(alignment: .leading, spacing: 2) {
                Text(senderLabel(message))
                  .font(.caption2)
                  .foregroundStyle(.secondary)
                  .lineLimit(1)
                Text(message.content).font(.footnote).lineLimit(6)
              }
              .frame(maxWidth: .infinity, alignment: .leading)
            }
            if let latestAgentReply {
              Button {
                speech.toggle(latestAgentReply)
              } label: {
                Label(
                  speech.isSpeaking ? "停止" : "朗读",
                  systemImage: speech.isSpeaking ? "stop.circle" : "speaker.wave.2"
                )
              }
            }
          }
        } else if isAwaitingReply {
          Text("等待回复…").font(.caption2).foregroundStyle(.secondary)
        }

        if let message = store.errorMessage {
          Text(message).font(.footnote).foregroundStyle(.orange)
        }
      }
      .padding(.vertical, 4)
    }
    .navigationTitle("发送")
    .task { messages = await store.recentMessages(sessionID: target.sessionID) }
    .onDisappear {
      pollTask?.cancel()
      speech.stop()
    }
  }

  private func send() {
    isSending = true
    let outgoing = trimmed
    let previousReply = latestAgentReply
    Task {
      let ok = await store.perform("send", on: target, text: outgoing)
      isSending = false
      guard ok else { return }
      text = ""
      sentNotice = "已发送"
      // 先把自己这句挂上去，别让用户盯着一屏没变化的记录等轮询。
      messages.append(.localEcho(outgoing))
      pollTask?.cancel()
      pollTask = Task { await awaitReply(after: previousReply) }
    }
  }

  /// 发出去之后等 agent 回一句，好让用户直接点朗读，不必再抬手翻聊天。
  /// 只在这个页面开着的时候轮询，离开就取消。
  private func awaitReply(after previous: String?) async {
    isAwaitingReply = true
    defer { isAwaitingReply = false }
    for _ in 0..<Self.replyPollRounds {
      try? await Task.sleep(nanoseconds: Self.replyPollInterval)
      if Task.isCancelled { return }
      let history = await store.recentMessages(sessionID: target.sessionID)
      let reply = history.last(where: \.isFromAgent)?.content
      // 拿到新回复才整体换掉列表：取空或没变化时保留乐观追加的那句。
      if let reply, reply != previous {
        messages = history
        return
      }
    }
  }
}
