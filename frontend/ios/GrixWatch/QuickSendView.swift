import SwiftUI

/// 快速发送：挑一个 agent 的最近会话，听写一句话发过去。
/// 发送走 owner-action 的 `send`，和手机上打字发消息是同一条落库路径。
struct QuickSendView: View {
  @EnvironmentObject private var store: WatchStore

  var body: some View {
    NavigationStack {
      List {
        if store.needsResync {
          ResyncNotice()
        } else if store.agents.isEmpty {
          Text(store.isLoading ? "加载中…" : "还没有可发送的会话")
            .foregroundStyle(.secondary)
        } else {
          ForEach(store.agents) { agent in
            NavigationLink(destination: QuickSendComposer(target: agent)) {
              VStack(alignment: .leading, spacing: 2) {
                Text(agent.agentName).font(.headline).lineLimit(1)
                Text(agent.displayTitle).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
              }
            }
          }
        }
      }
      .navigationTitle("快速发送")
      .refreshable { await store.refresh() }
    }
  }
}

struct QuickSendComposer: View {
  let target: ChatState
  @EnvironmentObject private var store: WatchStore
  @Environment(\.dismiss) private var dismiss

  @State private var text = ""
  @State private var lastReply: String?
  @State private var isSending = false

  private var trimmed: String {
    text.trimmingCharacters(in: .whitespacesAndNewlines)
  }

  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 10) {
        Text(target.agentName).font(.headline).lineLimit(1)

        if let lastReply {
          VStack(alignment: .leading, spacing: 2) {
            Text("最近回复").font(.caption2).foregroundStyle(.secondary)
            Text(lastReply).font(.footnote).lineLimit(4)
          }
        }

        TextField("说出要发送的内容", text: $text)
        Button {
          send()
        } label: {
          Label(isSending ? "发送中…" : "发送", systemImage: "paperplane")
        }
        .disabled(trimmed.isEmpty || isSending)

        if let message = store.errorMessage {
          Text(message).font(.footnote).foregroundStyle(.orange)
        }
      }
      .padding(.vertical, 4)
    }
    .navigationTitle("发送")
    .task { await loadLastReply() }
  }

  private func loadLastReply() async {
    // 只是给用户一点上下文，取不到就不显示，不打扰发送流程。
    lastReply = try? await store.api.lastAgentReply(sessionID: target.sessionID)
  }

  private func send() {
    isSending = true
    Task {
      let ok = await store.perform("send", on: target, text: trimmed)
      isSending = false
      if ok { dismiss() }
    }
  }
}
