import SwiftUI

/// Agent 一览：名字、在线、当前状态。一眼确认"谁在干活、谁卡住了"，
/// 点进去看这个 agent 名下的所有会话。
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
            NavigationLink(destination: AgentSessionsView(agent: agent)) {
              HStack(spacing: 8) {
                // 远程模型不接连接器，没有在线概念，不画状态点。
                if agent.reportsPresence {
                  Circle()
                    .fill(agent.agentOnline ? Color.green : Color.gray)
                    .frame(width: 8, height: 8)
                }
                VStack(alignment: .leading, spacing: 2) {
                  Text(agent.agentName).font(.headline).lineLimit(1)
                  Text("\(agent.stateLabel) · \(store.sessionCount(ofAgent: agent.agentID)) 个会话")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                }
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

/// 一个 agent 名下的会话列表：Agent 一览点进来的第二层，再点一条就是发送页。
/// 数据全部来自已经拉回来的 `states`，不额外请求、不轮询。
struct AgentSessionsView: View {
  let agent: ChatState
  @EnvironmentObject private var store: WatchStore

  private var sessions: [ChatState] { store.sessions(ofAgent: agent.agentID) }

  var body: some View {
    List {
      if sessions.isEmpty {
        Text("还没有会话").foregroundStyle(.secondary)
      } else {
        ForEach(sessions) { session in
          NavigationLink(destination: QuickSendComposer(target: session)) {
            VStack(alignment: .leading, spacing: 2) {
              Text(session.displayTitle).font(.headline).lineLimit(2)
              Text("\(session.stateLabel) · \(session.relativeUpdatedText)")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            }
          }
        }
      }
    }
    .navigationTitle(agent.agentName)
    .refreshable { await store.refresh() }
  }
}
