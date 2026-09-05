import SwiftUI

/// 收件箱：只列"卡在主人身上"的会话 —— 待审批与待回答。
struct InboxView: View {
  @EnvironmentObject private var store: WatchStore

  var body: some View {
    NavigationStack {
      List {
        if store.needsResync {
          ResyncNotice()
        } else if store.inbox.isEmpty {
          Text(store.isLoading ? "加载中…" : "没有待处理的事")
            .foregroundStyle(.secondary)
        } else {
          ForEach(store.inbox) { item in
            NavigationLink(destination: InboxItemView(item: item)) {
              InboxRow(item: item)
            }
          }
        }
        if let message = store.errorMessage, !store.needsResync {
          Text(message).font(.footnote).foregroundStyle(.orange)
        }
      }
      .navigationTitle("待处理")
      .refreshable { await store.refresh() }
    }
  }
}

struct InboxRow: View {
  let item: ChatState

  var body: some View {
    VStack(alignment: .leading, spacing: 2) {
      Text(item.displayTitle).font(.headline).lineLimit(2)
      Label(
        item.state == "waiting_approval" ? "待审批" : "待回答",
        systemImage: item.state == "waiting_approval" ? "hand.raised" : "questionmark.bubble"
      )
      .font(.caption2)
      .foregroundStyle(.secondary)
      Text(item.agentName).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
    }
  }
}

/// 一条待办的处置页：批准 / 拒绝 / 停止 / 听写回复。
struct InboxItemView: View {
  let item: ChatState
  @EnvironmentObject private var store: WatchStore
  @Environment(\.dismiss) private var dismiss

  @State private var replyText = ""
  @State private var busyAction: String?

  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 10) {
        Text(item.displayTitle).font(.headline)
        Text(item.agentName).font(.caption2).foregroundStyle(.secondary)

        if item.state == "waiting_approval" {
          actionButton("批准", systemImage: "checkmark", action: "approve", tint: .green)
          actionButton("拒绝", systemImage: "xmark", action: "deny", tint: .red)
        } else {
          // 手表上直接用系统输入界面听写，不引入任何第三方依赖。
          TextField("说出回复", text: $replyText)
          Button {
            run("reply", text: replyText)
          } label: {
            Label("发送回复", systemImage: "arrow.up.circle")
          }
          .disabled(replyText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || busyAction != nil)
        }

        actionButton("停止任务", systemImage: "stop.circle", action: "stop", tint: .orange)

        if let message = store.errorMessage {
          Text(message).font(.footnote).foregroundStyle(.orange)
        }
      }
      .padding(.vertical, 4)
    }
    .navigationTitle("处置")
  }

  private func actionButton(_ title: String, systemImage: String, action: String, tint: Color) -> some View {
    Button {
      run(action)
    } label: {
      Label(busyAction == action ? "处理中…" : title, systemImage: systemImage)
    }
    .tint(tint)
    .disabled(busyAction != nil)
  }

  private func run(_ action: String, text: String? = nil) {
    busyAction = action
    Task {
      let ok = await store.perform(action, on: item, text: text)
      busyAction = nil
      if ok { dismiss() }
    }
  }
}
