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
                Text(agent.stateLabel).font(.caption2).foregroundStyle(.secondary)
              }
            }
          }
        }
      }
      .navigationTitle("Agent")
      .refreshable { await store.refresh() }
    }
  }
}
