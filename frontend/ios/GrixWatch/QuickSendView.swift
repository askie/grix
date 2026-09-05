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

  @State private var text = ""
  @State private var lastReply: String?
  @State private var isSending = false
  @State private var isAwaitingReply = false
  @State private var sentNotice: String?
  @State private var pollTask: Task<Void, Never>?

  private var trimmed: String {
    text.trimmingCharacters(in: .whitespacesAndNewlines)
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

        if let lastReply {
          VStack(alignment: .leading, spacing: 6) {
            Text(isAwaitingReply ? "等待新回复…" : "最近回复")
              .font(.caption2)
              .foregroundStyle(.secondary)
            Text(lastReply).font(.footnote).lineLimit(6)
            Button {
              speech.toggle(lastReply)
            } label: {
              Label(
                speech.isSpeaking ? "停止" : "朗读",
                systemImage: speech.isSpeaking ? "stop.circle" : "speaker.wave.2"
              )
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
    .task { lastReply = await store.lastAgentReply(sessionID: target.sessionID) }
    .onDisappear {
      pollTask?.cancel()
      speech.stop()
    }
  }

  private func send() {
    isSending = true
    let previousReply = lastReply
    Task {
      let ok = await store.perform("send", on: target, text: trimmed)
      isSending = false
      guard ok else { return }
      text = ""
      sentNotice = "已发送"
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
      let next = await store.lastAgentReply(sessionID: target.sessionID)
      if let next, next != previous {
        lastReply = next
        return
      }
    }
  }
}
