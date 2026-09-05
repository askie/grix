import SwiftUI

/// Agent 一览：名字、在线、当前状态。一眼确认"谁在干活、谁卡住了"。
struct AgentsView: View {
  @EnvironmentObject private var store: WatchStore

  var body: some View {
    NavigationStack {
      List {
        if store.needsResync {
          ResyncNotice()
        } else if store.agents.isEmpty {
          Text(store.isLoading ? "加载中…" : "还没有任务记录")
            .foregroundStyle(.secondary)
        } else {
          ForEach(store.agents) { agent in
            HStack(spacing: 8) {
              // 远程模型不接连接器，没有在线概念，不画状态点。
              if agent.reportsPresence {
                Circle()
                  .fill(agent.agentOnline ? Color.green : Color.gray)
                  .frame(width: 8, height: 8)
              }
              VStack(alignment: .leading, spacing: 2) {
                Text(agent.agentName).font(.headline).lineLimit(1)
                Text(stateLabel(agent.state)).font(.caption2).foregroundStyle(.secondary)
              }
            }
          }
        }
      }
      .navigationTitle("Agent")
      .refreshable { await store.refresh() }
    }
  }

  private func stateLabel(_ state: String) -> String {
    switch state {
    case "running": return "运行中"
    case "waiting_approval": return "待审批"
    case "waiting_question": return "待回答"
    case "completed": return "已完成"
    case "failed": return "失败"
    default: return "空闲"
    }
  }
}
